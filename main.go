package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"text/template"

	"github.com/gin-gonic/gin"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	var err error
	var config *rest.Config

	var kubeconfig = flag.String("kubeconfig", "", "(optional) absolute path to the kubeconfig file")
	var namespaceFlag = flag.String("namespace", "", "(optional) namespace to use")
	var jobSpecPath = flag.String("job-spec", "", "path to job spec file")
	var serviceSpecPath = flag.String("service-spec", "", "(optional) path to service spec file")
	var ingressSpecPath = flag.String("ingress-spec", "", "(optional) path to ingress spec file")
	flag.Parse()

	var (
		jobTemplate     *template.Template
		serviceTemplate *template.Template
		ingressTemplate *template.Template
	)

	// Read template files
	if *jobSpecPath != "" {
		jobTemplateStr, err := ReadToString(*jobSpecPath)
		if err != nil {
			log.Fatalf("error reading job spec file: %v", err)
		}
		if jobTemplate, err = template.New("job").Parse(jobTemplateStr); err != nil {
			log.Fatalf("error parsing job template: %v", err)
		}
	} else {
		log.Fatalf("job-spec flag is required")
	}

	if *serviceSpecPath != "" {
		serviceTemplateStr, err := ReadToString(*serviceSpecPath)
		if err != nil {
			log.Fatalf("error reading service spec file: %v", err)
		}
		if serviceTemplate, err = template.New("service").Parse(serviceTemplateStr); err != nil {
			log.Fatalf("error parsing service template: %v", err)
		}
	}

	if *ingressSpecPath != "" {
		ingressTemplateStr, err := ReadToString(*ingressSpecPath)
		if err != nil {
			log.Fatalf("error reading ingress spec file: %v", err)
		}
		if ingressTemplate, err = template.New("ingress").Parse(ingressTemplateStr); err != nil {
			log.Fatalf("error parsing ingress template: %v", err)
		}
	}

	// Get the kubeconfig file path from flag, or use the in-cluster config
	if *kubeconfig == "" {
		log.Printf("Reading in-cluster configuration because kubeconfig flag is not set")
		config, err = rest.InClusterConfig()
	} else {
		log.Printf("Reading configuration from file: %s", *kubeconfig)
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	}
	if err != nil {
		panic(fmt.Errorf("error building kubeconfig: %v", err))
	}

	// Create the clientset
	log.Printf("Creating clientset")
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(fmt.Errorf("error building kubernetes clientset: %v", err))
	}

	// Get the current namespace
	var namespace string
	if *namespaceFlag != "" {
		namespace = *namespaceFlag
	} else {
		namespace = GetCurrentNamespaceOrDefault()
	}
	log.Printf("Using namespace: %s", namespace)

	// Create clients
	jobClient := clientset.BatchV1().Jobs(namespace)
	serviceClient := clientset.CoreV1().Services(namespace)
	ingressClient := clientset.NetworkingV1().Ingresses(namespace)

	// Set up services
	launcherService := NewLauncherService(
		jobClient,
		serviceClient,
		ingressClient,
		jobTemplate,
		serviceTemplate,
		ingressTemplate,
	)

	// Start listening for events
	go func() {
		if err := launcherService.CleanupWatcher(context.Background()); err != nil {
			log.Fatalf("error watching for cleanup events: %v", err)
		}
	}()

	// Set up webserver
	r := gin.Default()

	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"app":    "live-launcher",
		})
	})

	r.PUT("/api/v1/live/:videoId", func(c *gin.Context) {
		videoId := strings.Trim(c.Param("videoId"), "/")
		ctx := c.Request.Context()
		if err := launcherService.Launch(ctx, videoId); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	log.Printf("Starting webserver")
	r.Run()
}
