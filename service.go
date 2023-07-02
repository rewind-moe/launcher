package main

import (
	"context"
	"fmt"
	"log"
	"text/template"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	typedbatchv1 "k8s.io/client-go/kubernetes/typed/batch/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	typednetworkingv1 "k8s.io/client-go/kubernetes/typed/networking/v1"
)

type LauncherService struct {
	JobClient     typedbatchv1.JobInterface
	ServiceClient typedcorev1.ServiceInterface
	IngressClient typednetworkingv1.IngressInterface

	JobTemplate     *template.Template
	ServiceTemplate *template.Template
	IngressTemplate *template.Template
}

func NewLauncherService(
	jobClient typedbatchv1.JobInterface,
	serviceClient typedcorev1.ServiceInterface,
	ingressClient typednetworkingv1.IngressInterface,
	jobTemplate *template.Template,
	serviceTemplate *template.Template,
	ingressTemplate *template.Template,
) *LauncherService {
	return &LauncherService{
		JobClient:     jobClient,
		ServiceClient: serviceClient,
		IngressClient: ingressClient,

		JobTemplate:     jobTemplate,
		ServiceTemplate: serviceTemplate,
		IngressTemplate: ingressTemplate,
	}
}

func (s *LauncherService) launchJob(ctx context.Context, spec *TemplateSpec) (*batchv1.Job, error) {
	job, err := NewJobFromTemplate(s.JobTemplate, spec)
	if err != nil {
		return nil, fmt.Errorf("error creating job from template: %w", err)
	}

	j, err := s.JobClient.Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("error creating job %#v: %w", job, err)
	}

	return j, nil
}

func (s *LauncherService) launchService(ctx context.Context, spec *TemplateSpec) (*corev1.Service, error) {
	service, err := NewServiceFromTemplate(s.ServiceTemplate, spec)
	if err != nil {
		return nil, fmt.Errorf("error creating service from template: %w", err)
	}

	service, err = s.ServiceClient.Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("error creating service: %w", err)
	}

	return service, nil
}

func (s *LauncherService) launchIngress(ctx context.Context, spec *TemplateSpec) (*networkingv1.Ingress, error) {
	ingress, err := NewIngressFromTemplate(s.IngressTemplate, spec)
	if err != nil {
		return nil, fmt.Errorf("error creating ingress from template: %w", err)
	}

	ingress, err = s.IngressClient.Create(ctx, ingress, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("error creating ingress: %w", err)
	}

	return ingress, nil
}

func (s *LauncherService) Launch(ctx context.Context, videoId string) error {
	if videoId == "" {
		return fmt.Errorf("video ID cannot be empty")
	}

	spec := &TemplateSpec{
		VideoId: videoId,
	}

	if s.JobTemplate != nil {
		if _, err := s.launchJob(ctx, spec); err != nil {
			return fmt.Errorf("error creating job: %w", err)
		}
	}
	if s.ServiceTemplate != nil {
		if _, err := s.launchService(ctx, spec); err != nil {
			return fmt.Errorf("error creating service: %w", err)
		}
	}
	if s.IngressTemplate != nil {
		if _, err := s.launchIngress(ctx, spec); err != nil {
			return fmt.Errorf("error creating ingress: %w", err)
		}
	}

	return nil
}

func (s *LauncherService) CleanupWatcher(ctx context.Context) error {
	var labelSelector string
	for k, v := range DefaultLabels {
		labelSelector += fmt.Sprintf("%s=%s,", k, v)
	}
	labelSelector = labelSelector[:len(labelSelector)-1]

	// Start watching for jobs
	watch, err := s.JobClient.Watch(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("error watching jobs: %w", err)
	}

	for event := range watch.ResultChan() {
		job, ok := event.Object.(*batchv1.Job)
		if !ok {
			log.Printf("CleanupWatcher got unexpected object type: %T", event.Object)
			continue
		}

		if job.Status.Succeeded > 0 {
			// Job has completed, delete the associated service and/or ingress
			videoLabelSelector := labelSelector + fmt.Sprintf(",%s=%s", VideoIdLabel, job.Labels[VideoIdLabel])
			log.Printf("job %s has completed, deleting associated service and ingress", job.Name)

			// Find the service
			service, err := s.ServiceClient.List(ctx, metav1.ListOptions{
				LabelSelector: videoLabelSelector,
			})
			if err == nil {
				// Delete the service
				for _, svc := range service.Items {
					if err := s.ServiceClient.Delete(ctx, svc.Name, metav1.DeleteOptions{}); err != nil {
						log.Printf("error deleting service: %v", err)
					}
				}
			} else {
				log.Printf("error listing services: %v", err)
			}

			// Find the ingress
			ingress, err := s.IngressClient.List(ctx, metav1.ListOptions{
				LabelSelector: videoLabelSelector,
			})
			if err == nil {
				// Delete the ingress
				for _, ing := range ingress.Items {
					if err := s.IngressClient.Delete(ctx, ing.Name, metav1.DeleteOptions{}); err != nil {
						log.Printf("error deleting ingress: %v", err)
					}
				}
			} else {
				log.Printf("error listing ingress: %v", err)
			}
		}
	}

	return nil
}
