package minikube

import (
	"context"
	"sync"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/scott-the-programmer/terraform-provider-minikube/minikube/lib"
)

func init() {
	schema.DescriptionKind = schema.StringMarkdown
}

func Provider() *schema.Provider {
	return NewProvider(providerConfigure)
}

func NewProvider(providerConfigure schema.ConfigureContextFunc) *schema.Provider {
	return &schema.Provider{
		ResourcesMap: map[string]*schema.Resource{
			"minikube_cluster": ResourceCluster(),
		},
		DataSourcesMap:       map[string]*schema.Resource{},
		ConfigureContextFunc: providerConfigure,
		Schema: map[string]*schema.Schema{
			"kubernetes_version": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The Kubernetes version that the minikube VM will use. Defaults to 'v1.30.0'.",
				Default:     "v1.30.0",
			},
			"docker_context": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Docker context to use for Docker operations. Supports remote contexts via TLS or SSH.",
				Default:     "",
			},
			"docker_host": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Docker daemon endpoint (e.g., tcp://192.168.1.100:2376 or ssh://user@host).",
				Default:     "",
			},
			"docker_cert_path": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Path to TLS certificates for Docker daemon connection.",
				Default:     "",
			},
			"docker_tls_verify": {
				Type:        schema.TypeBool,
				Optional:    true,
				Description: "Enable TLS verification for Docker daemon connection.",
				Default:     false,
			},
			"docker_platform": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Docker platform to use (e.g., linux/amd64, linux/arm64). If not set, Docker will auto-detect.",
				Default:     "",
			},
		},
	}
}

func providerConfigure(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
	var diags diag.Diagnostics

	mutex := &sync.Mutex{}
	k8sVersion := d.Get("kubernetes_version").(string)
	dockerContext := d.Get("docker_context").(string)
	dockerHost := d.Get("docker_host").(string)
	dockerCertPath := d.Get("docker_cert_path").(string)
	dockerTLSVerify := d.Get("docker_tls_verify").(bool)
	dockerPlatform := d.Get("docker_platform").(string)
	
	minikubeClientFactory := func() (lib.ClusterClient, error) {
		client := &lib.MinikubeClient{
			TfCreationLock:  mutex,
			K8sVersion:      k8sVersion,
			DockerContext:   dockerContext,
			DockerHost:      dockerHost,
			DockerCertPath:  dockerCertPath,
			DockerTLSVerify: dockerTLSVerify,
			DockerPlatform:  dockerPlatform,
		}
		// Wrap with caching layer for better performance
		return lib.NewCachedClusterClient(client), nil
	}
	return minikubeClientFactory, diags
}
