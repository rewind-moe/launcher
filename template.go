package main

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"text/template"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type TemplateSpec struct {
	VideoId string `json:"videoId"`

	UniqueName   string
	VideoIdLabel string
}

func GenTemplateSpec(spec *TemplateSpec) {
	// Hash video ID
	hash := sha1.Sum([]byte(spec.VideoId))
	hashString := fmt.Sprintf("%x", hash)

	spec.UniqueName = hashString[:8]
	spec.VideoIdLabel = VideoIdLabel
}

func NewJobFromTemplate(tmpl *template.Template, spec *TemplateSpec) (*batchv1.Job, error) {
	// Generate template
	GenTemplateSpec(spec)
	buf := &bytes.Buffer{}
	if err := tmpl.Execute(buf, spec); err != nil {
		return nil, fmt.Errorf("error executing job template: %w", err)
	}

	// Parse resulting YAML
	var job *batchv1.Job
	if err := yaml.NewYAMLOrJSONDecoder(buf, 100).Decode(&job); err != nil {
		return nil, fmt.Errorf("error parsing job YAML: %w", err)
	}

	// Add labels
	if job.Labels == nil {
		job.Labels = map[string]string{}
	}
	for k, v := range DefaultLabels {
		job.Labels[k] = v
	}
	job.Labels[VideoIdLabel] = spec.VideoId

	return job, nil
}

func NewServiceFromTemplate(tmpl *template.Template, spec *TemplateSpec) (*corev1.Service, error) {
	// Generate template
	GenTemplateSpec(spec)
	buf := &bytes.Buffer{}
	if err := tmpl.Execute(buf, spec); err != nil {
		return nil, fmt.Errorf("error executing service template: %w", err)
	}

	// Parse resulting YAML
	var service *corev1.Service
	if err := yaml.NewYAMLOrJSONDecoder(buf, 100).Decode(&service); err != nil {
		return nil, fmt.Errorf("error parsing service YAML: %w", err)
	}

	// Add labels
	if service.Labels == nil {
		service.Labels = map[string]string{}
	}
	for k, v := range DefaultLabels {
		service.Labels[k] = v
	}
	service.Labels[VideoIdLabel] = spec.VideoId

	return service, nil
}

func NewIngressFromTemplate(tmpl *template.Template, spec *TemplateSpec) (*networkingv1.Ingress, error) {
	// Generate template
	GenTemplateSpec(spec)
	buf := &bytes.Buffer{}
	if err := tmpl.Execute(buf, spec); err != nil {
		return nil, fmt.Errorf("error executing ingress template: %w", err)
	}

	// Parse resulting YAML
	var ingress *networkingv1.Ingress
	if err := yaml.NewYAMLOrJSONDecoder(buf, 100).Decode(&ingress); err != nil {
		return nil, fmt.Errorf("error parsing ingress YAML: %w", err)
	}

	// Add labels
	if ingress.Labels == nil {
		ingress.Labels = map[string]string{}
	}
	for k, v := range DefaultLabels {
		ingress.Labels[k] = v
	}
	ingress.Labels[VideoIdLabel] = spec.VideoId

	return ingress, nil
}
