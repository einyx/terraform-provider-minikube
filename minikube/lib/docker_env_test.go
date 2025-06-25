package lib

import (
	"os"
	"testing"
)

func TestSetDockerEnvironment(t *testing.T) {
	tests := []struct {
		name            string
		client          *MinikubeClient
		expectedEnvVars map[string]string
	}{
		{
			name: "all docker settings provided",
			client: &MinikubeClient{
				DockerContext:   "remote-context",
				DockerHost:      "tcp://192.168.1.100:2376",
				DockerCertPath:  "/path/to/certs",
				DockerTLSVerify: true,
			},
			expectedEnvVars: map[string]string{
				"DOCKER_CONTEXT":    "remote-context",
				"DOCKER_HOST":       "tcp://192.168.1.100:2376",
				"DOCKER_CERT_PATH":  "/path/to/certs",
				"DOCKER_TLS_VERIFY": "1",
			},
		},
		{
			name: "only docker host provided",
			client: &MinikubeClient{
				DockerHost: "ssh://user@host",
			},
			expectedEnvVars: map[string]string{
				"DOCKER_HOST": "ssh://user@host",
			},
		},
		{
			name:            "no docker settings provided",
			client:          &MinikubeClient{},
			expectedEnvVars: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear any existing Docker environment variables
			os.Unsetenv("DOCKER_CONTEXT")
			os.Unsetenv("DOCKER_HOST")
			os.Unsetenv("DOCKER_CERT_PATH")
			os.Unsetenv("DOCKER_TLS_VERIFY")

			// Set the Docker environment
			tt.client.setDockerEnvironment()

			// Verify expected environment variables are set
			for key, expectedValue := range tt.expectedEnvVars {
				actualValue := os.Getenv(key)
				if actualValue != expectedValue {
					t.Errorf("Expected %s=%s, got %s", key, expectedValue, actualValue)
				}
			}

			// Verify no unexpected environment variables are set
			dockerEnvVars := []string{"DOCKER_CONTEXT", "DOCKER_HOST", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY"}
			for _, key := range dockerEnvVars {
				if _, expected := tt.expectedEnvVars[key]; !expected {
					if value := os.Getenv(key); value != "" {
						t.Errorf("Unexpected %s=%s", key, value)
					}
				}
			}
		})
	}
}