package main

import (
	"fmt"
	"log"
	"os"
)

func ReadToString(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("error reading file %v: %w", path, err)
	}
	return string(data), nil
}

func GetCurrentNamespaceOrDefault() string {
	if ns, err := ReadToString("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err != nil {
		log.Printf("Error reading namespace file: %v", err)
		return "default"
	} else {
		return ns
	}
}
