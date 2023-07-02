package main

const (
	VideoIdLabel = "rewind.moe/video-id"
)

var (
	DefaultLabels = map[string]string{
		"app.kubernetes.io/managed-by": "rewind-launcher",
	}
)
