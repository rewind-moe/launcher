package tests

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func randomString(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

	b := make([]rune, n)
	for i := range b {
		r, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			panic(err)
		}
		b[i] = letters[r.Int64()]
	}
	return string(b)
}

func startMinikube(ctx context.Context, t *testing.T) {
	t.Log("starting minikube")
	cmd := exec.CommandContext(ctx, "minikube", "start")
	if err := cmd.Run(); err != nil {
		t.Fatalf("error starting minikube: %v", err)
	}
}

func buildContainerImage(ctx context.Context, t *testing.T, imageTag string) {
	// Get the minikube docker environment variables
	cmdEnv := exec.CommandContext(ctx, "minikube", "docker-env")
	env, err := cmdEnv.Output()
	if err != nil {
		t.Fatalf("error getting minikube docker-env: %v", err)
	}

	// Set the environment variables for the current process
	for _, line := range strings.Split(string(env), "\n") {
		if !strings.HasPrefix(line, "export ") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := strings.Trim(parts[1], "\"")
		t.Logf("setting environment variable %q to %q", key, value)
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("error setting environment variable %q: %v", key, err)
		}
	}

	t.Logf("building container image %q", imageTag)
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", imageTag, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("error building container image: %v", err)
	}
}

func getClientset(ctx context.Context, t *testing.T) *kubernetes.Clientset {
	assert := assert.New(t)

	kubeconfigPath := path.Join(homedir.HomeDir(), ".kube", "config")
	t.Logf("using kubeconfig at %q", kubeconfigPath)

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	assert.NoError(err, "error building config from flags")

	clientset, err := kubernetes.NewForConfig(config)
	assert.NoError(err, "error creating clientset")

	return clientset
}

func createNamespace(ctx context.Context, t *testing.T, clientset *kubernetes.Clientset, nsName string) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	if _, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		t.Fatalf("error creating namespace: %v", err)
	}
	t.Logf("created namespace %q", nsName)
}

func deleteNamespace(ctx context.Context, t *testing.T, clientset *kubernetes.Clientset, nsName string) {
	if err := clientset.CoreV1().Namespaces().Delete(ctx, nsName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("error deleting namespace: %v", err)
	}
	t.Logf("deleted namespace %q", nsName)
}

func getMinikubeIp(ctx context.Context, t *testing.T) string {
	cmd := exec.CommandContext(ctx, "minikube", "ip")
	ip, err := cmd.Output()
	if err != nil {
		t.Fatalf("error getting minikube ip: %v", err)
	}
	return strings.TrimSpace(string(ip))
}

func deployLauncherService(ctx context.Context, t *testing.T, clientset *kubernetes.Clientset, namespace string, imageTag string) string {
	var err error
	t.Logf("deploying launcher service with image tag %q", imageTag)

	// Deploy configmaps
	t.Logf("deploying configmaps")
	jobSpecConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "launcher-specs",
			Labels: map[string]string{
				"app": "launcher",
			},
		},
		Data: map[string]string{
			"job-spec.yaml": `
apiVersion: batch/v1
kind: Job
metadata:
  name: test-job-{{ .UniqueName }}
  labels:
    app: test-job
spec:
  backoffLimit: 4
  template:
    spec:
      restartPolicy: OnFailure
      containers:
      - name: success-in-10-seconds
        image: busybox
        args: ['/bin/sh', '-c', 'sleep 10']
        ports:
        - name: http
          containerPort: 8080
          protocol: TCP
`,
		},
	}
	if _, err = clientset.CoreV1().ConfigMaps(namespace).Create(ctx, jobSpecConfigMap, metav1.CreateOptions{}); err != nil {
		t.Fatalf("error creating job spec configmap: %v", err)
	}

	// Deploy service account
	t.Logf("deploying service account")
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: "launcher-service-account",
			Labels: map[string]string{
				"app": "launcher",
			},
		},
	}
	if serviceAccount, err = clientset.CoreV1().ServiceAccounts(namespace).Create(ctx, serviceAccount, metav1.CreateOptions{}); err != nil {
		t.Fatalf("error creating service account: %v", err)
	}

	// Deploy role
	t.Logf("deploying role")
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name: "launcher-role",
			Labels: map[string]string{
				"app": "launcher",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"batch"},
				Resources: []string{"jobs"},
				Verbs:     []string{"create", "get", "list", "watch", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
	if role, err = clientset.RbacV1().Roles(namespace).Create(ctx, role, metav1.CreateOptions{}); err != nil {
		t.Fatalf("error creating role: %v", err)
	}

	// Deploy rolebinding
	t.Logf("deploying rolebinding")
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "launcher-role-binding",
			Labels: map[string]string{
				"app": "launcher",
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccount.Name,
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     role.Name,
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	if _, err = clientset.RbacV1().RoleBindings(namespace).Create(ctx, roleBinding, metav1.CreateOptions{}); err != nil {
		t.Fatalf("error creating rolebinding: %v", err)
	}

	// Deploy pod
	t.Logf("deploying pod")
	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/",
				Port: intstr.FromInt(8080),
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "launcher",
			Labels: map[string]string{
				"app": "launcher",
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: serviceAccount.Name,
			Containers: []corev1.Container{
				{
					Name:            "launcher",
					Image:           imageTag,
					ImagePullPolicy: corev1.PullPolicy("Never"),
					StartupProbe:    probe,
					LivenessProbe:   probe,
					Command: []string{
						"/bin/app",
						"--job-spec",
						"/etc/launcher/job-spec.yaml",
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "launcher-specs",
							MountPath: "/etc/launcher",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "launcher-specs",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "launcher-specs",
							},
						},
					},
				},
			},
		},
	}
	if pod, err = clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("error creating pod: %v", err)
	}

	// Deploy service
	t.Logf("deploying service")
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "launcher",
			Labels: map[string]string{
				"app": "launcher",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: pod.Labels,
			Type:     corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     8080,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}
	if _, err := clientset.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
		t.Fatalf("error creating service: %v", err)
	}

	t.Logf("waiting for pod to be ready")

	var i int
	maxTry := 60
	for i = 0; i < maxTry; i++ {
		pod, err = clientset.CoreV1().Pods(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("error getting pod: %v", err)
		}

		var condition *corev1.PodCondition
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodReady {
				condition = &c
				break
			}
		}

		if condition != nil {
			t.Logf("current status: %q; PodReady: %v; try %d/%d", pod.Status.Phase, condition.Status, i+1, maxTry)
			if condition.Status == corev1.ConditionTrue {
				break
			}
		}

		time.Sleep(1 * time.Second)
	}

	if i == 30 {
		t.Fatalf("pod did not become ready")
	}

	t.Logf("pod is ready")

	svc, err = clientset.CoreV1().Services(namespace).Get(ctx, svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting service: %v", err)
	}

	return fmt.Sprintf("http://%s:%d", getMinikubeIp(ctx, t), svc.Spec.Ports[0].NodePort)
}

func TestEndToEnd(t *testing.T) {
	ctx := context.Background()

	// Change to the project root dir
	os.Chdir("..")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("error getting current working directory: %v", err)
	}
	t.Logf("current working directory is %q", cwd)

	// Make sure minikube is running
	startMinikube(ctx, t)

	// Get the k8s clientset
	clientset := getClientset(ctx, t)

	// Deploy a namespace for the test
	nsName := "test-" + randomString(8)
	createNamespace(ctx, t, clientset, nsName)
	defer deleteNamespace(ctx, t, clientset, nsName)

	// Build the container image
	containerImageTag := "launcher:latest"
	buildContainerImage(ctx, t, containerImageTag)

	// Deploy the launcher service
	svcAddr := deployLauncherService(ctx, t, clientset, nsName, containerImageTag)
	t.Logf("launcher service is available at %q", svcAddr)

	// Send health check
	t.Logf("sending health check")
	resp, err := http.Get(svcAddr)
	if err != nil {
		t.Fatalf("error sending health check: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health check returned status %q", resp.Status)
	}

	// Send a job
	t.Logf("sending job")
	videoId := randomString(11)

	// PUT request to create the job
	req, err := http.NewRequest(http.MethodPut, svcAddr+"/api/v1/live/"+videoId, nil)
	if err != nil {
		t.Fatalf("error creating request: %v", err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("error sending job: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Logf("response body: %s", body)
		t.Fatalf("job returned status %q", resp.Status)
	}

	// Check that the job is running
	t.Logf("checking that the job is running")
	var job *batchv1.Job
	maxTry := 60
	for i := 0; i < maxTry; i++ {
		jobs, err := clientset.BatchV1().Jobs(nsName).List(ctx, metav1.ListOptions{
			LabelSelector: "app=test-job",
		})
		if err != nil {
			t.Fatalf("error getting job: %v", err)
		}

		if len(jobs.Items) == 0 {
			t.Logf("job not found; try %d/%d", i+1, maxTry)
			time.Sleep(1 * time.Second)
			continue
		}

		job = &jobs.Items[0]
		if job.Status.Active == 0 {
			t.Logf("job is not running; try %d/%d", i+1, maxTry)
			time.Sleep(1 * time.Second)
			continue
		}

		break
	}

	t.Logf("job is running, waiting for it to complete")
	for i := 0; i < maxTry; i++ {
		job, err = clientset.BatchV1().Jobs(nsName).Get(ctx, job.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("error getting job: %v", err)
		}

		if job.Status.Active > 0 {
			t.Logf("job is still running; try %d/%d", i+1, maxTry)
			time.Sleep(1 * time.Second)
			continue
		}

		if job.Status.Succeeded == 0 {
			t.Fatalf("job failed")
		}

		break
	}

	t.Logf("job completed")
}
