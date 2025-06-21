//go:generate go run github.com/golang/mock/mockgen -source=$GOFILE -destination=mock_$GOFILE -package=$GOPACKAGE
package lib

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/docker/machine/libmachine/ssh"
	"github.com/spf13/viper"
	"k8s.io/klog/v2"
	cmdcfg "k8s.io/minikube/cmd/minikube/cmd/config"
	"k8s.io/minikube/pkg/minikube/config"
	"k8s.io/minikube/pkg/minikube/kubeconfig"
	"k8s.io/minikube/pkg/minikube/localpath"
	"k8s.io/minikube/pkg/minikube/node"
	"k8s.io/minikube/pkg/minikube/out/register"

	// Register drivers
	_ "k8s.io/minikube/pkg/minikube/registry/drvs"
)

const (
	Podman = "podman"
	Docker = "docker"

	MinExtraHANodes = 2
)

type ClusterClient interface {
	SetConfig(args MinikubeClientConfig)
	GetConfig() MinikubeClientConfig
	SetDependencies(dep MinikubeClientDeps)
	Start() (*kubeconfig.Settings, error)
	Delete() error
	GetClusterConfig() *config.ClusterConfig
	GetK8sVersion() string
	ApplyAddons(addons []string) error
	GetAddons() []string
}

type MinikubeClient struct {
	clusterConfig   *config.ClusterConfig
	clusterName     string
	addons          []string
	isoUrls         []string
	deleteOnFailure bool
	nodes           int
	ha              bool
	nativeSsh       bool

	// TfCreationLock is a mutex used to prevent multiple minikube clients from conflicting on Start().
	// Only set this if you're using MinikubeClient in a concurrent context
	TfCreationLock *sync.Mutex
	K8sVersion     string

	nRunner Cluster
	dLoader Downloader
}

type MinikubeClientConfig struct {
	ClusterConfig   *config.ClusterConfig
	ClusterName     string
	Addons          []string
	IsoUrls         []string
	DeleteOnFailure bool
	Nodes           int
	HA              bool
	NativeSsh       bool
}

type MinikubeClientDeps struct {
	Node       Cluster
	Downloader Downloader
}

// NewMinikubeClient creates a new MinikubeClient struct
func NewMinikubeClient(args MinikubeClientConfig, dep MinikubeClientDeps) *MinikubeClient {
	return &MinikubeClient{
		clusterConfig:   args.ClusterConfig,
		isoUrls:         args.IsoUrls,
		clusterName:     args.ClusterName,
		addons:          args.Addons,
		deleteOnFailure: args.DeleteOnFailure,
		TfCreationLock:  nil,
		nodes:           args.Nodes,
		nativeSsh:       args.NativeSsh,
		ha:              args.HA,

		nRunner: dep.Node,
		dLoader: dep.Downloader,
	}
}

func init() {
	registerLogging()
	klog.V(klog.Level(1))

	targetDir := localpath.MakeMiniPath("bin")
	new := fmt.Sprintf("%s:%s", targetDir, os.Getenv("PATH"))
	os.Setenv("PATH", new)

	register.Reg.SetStep(register.InitialSetup)

}

// SetConfig sets the clients configuration
func (e *MinikubeClient) SetConfig(args MinikubeClientConfig) {
	e.clusterConfig = args.ClusterConfig
	e.isoUrls = args.IsoUrls
	e.clusterName = args.ClusterName
	e.addons = args.Addons
	e.deleteOnFailure = args.DeleteOnFailure
	e.nodes = args.Nodes
	e.nativeSsh = args.NativeSsh
	e.ha = args.HA
}

// GetConfig retrieves the current clients configuration
func (e *MinikubeClient) GetConfig() MinikubeClientConfig {
	return MinikubeClientConfig{
		ClusterConfig:   e.clusterConfig,
		IsoUrls:         e.isoUrls,
		ClusterName:     e.clusterName,
		Addons:          e.addons,
		DeleteOnFailure: e.deleteOnFailure,
		Nodes:           e.nodes,
		HA:              e.ha,
	}
}

// SetDependencies injects dependencies into the MinikubeClient
func (e *MinikubeClient) SetDependencies(dep MinikubeClientDeps) {
	e.nRunner = dep.Node
	e.dLoader = dep.Downloader
}

// Start starts the minikube creation process. If the cluster already exists, it will attempt to reuse it
func (e *MinikubeClient) Start() (*kubeconfig.Settings, error) {

	// By nature, viper references (here and within the internals of minikube) are not thread safe.
	// To keep our sanity, let's mutex this call and defer subsequent cluster starts
	if e.TfCreationLock != nil {
		e.TfCreationLock.Lock()
		defer e.TfCreationLock.Unlock()
	}

	viper.Set(cmdcfg.Bootstrapper, "kubeadm")
	viper.Set(config.ProfileName, e.clusterName)
	viper.Set("preload", true)
	viper.Set("ha", e.ha)

	url, err := e.downloadIsos()
	if err != nil {
		return nil, err
	}

	e.clusterConfig.MinikubeISO = url
	if e.clusterConfig.Driver == Podman || e.clusterConfig.Driver == Docker { // use volume mounts for container runtimes
		e.clusterConfig.ContainerVolumeMounts = []string{e.clusterConfig.MountString}
	}

	if e.nativeSsh {
		ssh.SetDefaultClient(ssh.Native)
	} else {
		ssh.SetDefaultClient(ssh.External)
	}

	mRunner, preExists, mAPI, host, err := e.nRunner.Provision(e.clusterConfig, &e.clusterConfig.Nodes[0], true)
	if err != nil {
		return nil, err
	}
	starter := node.Starter{
		Runner:         mRunner,
		PreExists:      preExists,
		StopK8s:        false,
		MachineAPI:     mAPI,
		Host:           host,
		Cfg:            e.clusterConfig,
		Node:           &e.clusterConfig.Nodes[0],
		ExistingAddons: e.clusterConfig.Addons,
	}

	kc, err := e.nRunner.Start(starter)
	if err != nil {
		return nil, err
	}

	e.clusterConfig, err = e.addHANodes(e.clusterConfig)
	if err != nil {
		return nil, err
	}

	err = e.provisionNodes(starter)
	if err != nil {
		return nil, err
	}

	klog.Flush()

	e.setAddons(e.addons, true)

	return kc, nil
}

func (e *MinikubeClient) addHANodes(cc *config.ClusterConfig) (*config.ClusterConfig, error) {
	if e.ha && e.nodes-1 < MinExtraHANodes { // excluding the initial node
		return nil, errors.New("you need at least 3 nodes for high availability")
	}

	var err error
	if e.ha {
		// Note: Control plane nodes must be added sequentially to maintain cluster state consistency
		// Each control plane node modifies the cluster config which is needed for the next node
		for i := 0; i < MinExtraHANodes; i++ {
			cc, err = e.nRunner.AddControlPlaneNode(cc,
				cc.KubernetesConfig.KubernetesVersion,
				cc.APIServerPort,
				cc.KubernetesConfig.ContainerRuntime)
			if err != nil {
				return nil, err
			}
		}

		e.nodes -= MinExtraHANodes
	}

	return cc, nil

}

func (e *MinikubeClient) provisionNodes(starter node.Starter) error {
	// Remaining nodes
	nodeCount := e.nodes - 1 // excluding the initial node
	if nodeCount <= 0 {
		return nil
	}

	// For small clusters (3 or fewer worker nodes), use sequential provisioning
	// For larger clusters, use parallel provisioning with a worker pool
	if nodeCount <= 3 {
		for i := 0; i < nodeCount; i++ {
			err := e.nRunner.AddWorkerNode(e.clusterConfig,
				starter.Cfg.KubernetesConfig.KubernetesVersion,
				starter.Cfg.APIServerPort,
				starter.Cfg.KubernetesConfig.ContainerRuntime)
			if err != nil {
				return err
			}
		}
		return nil
	}

	// Parallel provisioning for larger clusters
	type result struct {
		index int
		err   error
	}

	// Use a worker pool with max 4 concurrent provisioning operations
	maxWorkers := 4
	if nodeCount < maxWorkers {
		maxWorkers = nodeCount
	}

	jobs := make(chan int, nodeCount)
	results := make(chan result, nodeCount)

	// Start workers
	var wg sync.WaitGroup
	for w := 0; w < maxWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				err := e.nRunner.AddWorkerNode(e.clusterConfig,
					starter.Cfg.KubernetesConfig.KubernetesVersion,
					starter.Cfg.APIServerPort,
					starter.Cfg.KubernetesConfig.ContainerRuntime)
				results <- result{index: i, err: err}
			}
		}()
	}

	// Queue jobs
	for i := 0; i < nodeCount; i++ {
		jobs <- i
	}
	close(jobs)

	// Wait for all workers to finish
	wg.Wait()
	close(results)

	// Check for errors
	for r := range results {
		if r.err != nil {
			return fmt.Errorf("failed to provision worker node %d: %w", r.index, r.err)
		}
	}

	return nil
}

func (e *MinikubeClient) ApplyAddons(addons []string) error {

	// By nature, viper references (here and within the internals of minikube) are not thread safe.
	// To keep our sanity, let's mutex this call and defer subsequent cluster starts
	if e.TfCreationLock != nil {
		e.TfCreationLock.Lock()
		defer e.TfCreationLock.Unlock()
	}

	viper.Set(config.ProfileName, e.clusterName)

	addonsToDelete := diff(e.addons, addons)
	err := e.setAddons(addonsToDelete, false)
	if err != nil {
		return err
	}

	addonsToAdd := diff(addons, e.addons)
	err = e.setAddons(addonsToAdd, true)
	if err != nil {
		return err
	}

	e.addons = addons

	return nil
}

func (e *MinikubeClient) GetAddons() []string {
	addons := make([]string, 0)
	for addon, enabled := range e.GetClusterConfig().Addons {
		if enabled {
			addons = append(addons, addon)
		}
	}

	return addons
}

func diff(addonsA, addonsB []string) []string {
	lookupB := make(map[string]struct{}, len(addonsB))
	for _, addon := range addonsB {
		lookupB[addon] = struct{}{}
	}
	var diff []string
	for _, addon := range addonsA {
		if _, found := lookupB[addon]; !found {
			diff = append(diff, addon)
		}
	}
	return diff
}

func (e *MinikubeClient) setAddons(addons []string, val bool) error {
	if len(addons) == 0 {
		return nil
	}

	// For small number of addons, use sequential approach
	if len(addons) <= 2 {
		for _, addon := range addons {
			err := e.nRunner.SetAddon(e.clusterName, addon, strconv.FormatBool(val))
			if err != nil {
				return err
			}
		}
		return nil
	}

	// For larger number of addons, use concurrent approach with limited parallelism
	// to avoid overwhelming the minikube API
	type result struct {
		addon string
		err   error
	}

	// Limit concurrency to 3 to avoid overwhelming minikube
	maxWorkers := 3
	if len(addons) < maxWorkers {
		maxWorkers = len(addons)
	}

	jobs := make(chan string, len(addons))
	results := make(chan result, len(addons))

	// Start workers
	var wg sync.WaitGroup
	for w := 0; w < maxWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for addon := range jobs {
				err := e.nRunner.SetAddon(e.clusterName, addon, strconv.FormatBool(val))
				results <- result{addon: addon, err: err}
			}
		}()
	}

	// Queue jobs
	for _, addon := range addons {
		jobs <- addon
	}
	close(jobs)

	// Wait for all workers to finish
	wg.Wait()
	close(results)

	// Collect errors
	var firstErr error
	failedAddons := []string{}
	for r := range results {
		if r.err != nil {
			failedAddons = append(failedAddons, r.addon)
			if firstErr == nil {
				firstErr = r.err
			}
		}
	}

	if firstErr != nil {
		return fmt.Errorf("failed to set addons %v: %w", failedAddons, firstErr)
	}

	return nil
}

// Delete deletes the given cluster associated with the cluster config
func (e *MinikubeClient) Delete() error {
	_, err := e.nRunner.Delete(e.clusterConfig, e.clusterName)
	if err != nil {
		return err
	}
	return nil
}

// GetClusterConfig retrieves the latest cluster config from minikube
func (e *MinikubeClient) GetClusterConfig() *config.ClusterConfig {
	return e.nRunner.Get(e.clusterName)
}

func (e *MinikubeClient) GetK8sVersion() string {
	return e.K8sVersion
}

// downloadIsos retrieve all prerequisite images prior to provisioning
func (e *MinikubeClient) downloadIsos() (string, error) {
	// Check if downloader supports parallel downloads
	if pd, ok := e.dLoader.(*ParallelDownloader); ok {
		return pd.DownloadAll(e.isoUrls, true,
			e.clusterConfig.KubernetesConfig.KubernetesVersion,
			e.clusterConfig.KubernetesConfig.ContainerRuntime,
			e.clusterConfig.Driver)
	}

	// Fallback to sequential downloads
	url, err := e.dLoader.ISO(e.isoUrls, true)
	if err != nil {
		return "", err
	}

	err = e.dLoader.PreloadTarball(e.clusterConfig.KubernetesConfig.KubernetesVersion,
		e.clusterConfig.KubernetesConfig.ContainerRuntime,
		e.clusterConfig.Driver)
	if err != nil {
		return "", err
	}

	return url, nil
}
