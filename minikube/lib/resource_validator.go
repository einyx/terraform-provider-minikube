package lib

import (
	"errors"
	"fmt"
	"runtime"

	"k8s.io/minikube/pkg/minikube/config"
)

// ResourceValidator provides efficient validation of cluster resources
type ResourceValidator struct {
	// Minimum resources based on minikube requirements
	minMemoryMB int
	minCPUs     int
	minDiskMB   int
}

// NewResourceValidator creates a new resource validator
func NewResourceValidator() *ResourceValidator {
	return &ResourceValidator{
		minMemoryMB: 2048,  // 2GB minimum
		minCPUs:     2,     // 2 CPUs minimum
		minDiskMB:   20480, // 20GB minimum
	}
}

// ValidateResources performs comprehensive resource validation
func (v *ResourceValidator) ValidateResources(cc *config.ClusterConfig) error {
	var errs []error

	// Validate memory
	if err := v.validateMemory(cc.Memory); err != nil {
		errs = append(errs, err)
	}

	// Validate CPUs
	if err := v.validateCPUs(cc.CPUs); err != nil {
		errs = append(errs, err)
	}

	// Validate disk size
	if err := v.validateDisk(cc.DiskSize); err != nil {
		errs = append(errs, err)
	}

	// Validate driver-specific resources
	if err := v.validateDriverResources(cc); err != nil {
		errs = append(errs, err)
	}

	// Validate node configuration
	if err := v.validateNodeConfiguration(cc); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func (v *ResourceValidator) validateMemory(memoryMB int) error {
	if memoryMB < v.minMemoryMB {
		return fmt.Errorf("memory must be at least %dMB, got %dMB", v.minMemoryMB, memoryMB)
	}

	// Check available system memory
	memInfo, err := GetMemoryLimit()
	if err == nil && memInfo.SystemMemory > 0 && memoryMB > int(float64(memInfo.SystemMemory)*0.8) {
		return fmt.Errorf("requested memory %dMB exceeds 80%% of available system memory (%dMB)", memoryMB, memInfo.SystemMemory)
	}

	return nil
}

func (v *ResourceValidator) validateCPUs(cpus int) error {
	if cpus < v.minCPUs {
		return fmt.Errorf("CPUs must be at least %d, got %d", v.minCPUs, cpus)
	}

	// Check available system CPUs
	maxCPUs := runtime.NumCPU()
	if cpus > maxCPUs {
		return fmt.Errorf("requested CPUs %d exceeds system CPUs %d", cpus, maxCPUs)
	}

	return nil
}

func (v *ResourceValidator) validateDisk(diskMB int) error {
	if diskMB < v.minDiskMB {
		return fmt.Errorf("disk size must be at least %dMB, got %dMB", v.minDiskMB, diskMB)
	}

	return nil
}

func (v *ResourceValidator) validateDriverResources(cc *config.ClusterConfig) error {
	switch cc.Driver {
	case "docker", "podman":
		// Container drivers have different resource requirements
		if cc.Memory < 1024 {
			return fmt.Errorf("%s driver requires at least 1GB memory", cc.Driver)
		}
	case "virtualbox", "vmware", "hyperkit":
		// VM drivers need more resources
		if cc.Memory < 2048 {
			return fmt.Errorf("%s driver requires at least 2GB memory", cc.Driver)
		}
		if cc.CPUs < 2 {
			return fmt.Errorf("%s driver requires at least 2 CPUs", cc.Driver)
		}
	}

	return nil
}

func (v *ResourceValidator) validateNodeConfiguration(cc *config.ClusterConfig) error {
	nodeCount := len(cc.Nodes)

	// Calculate total resources needed
	totalMemory := cc.Memory * nodeCount
	totalCPUs := cc.CPUs * nodeCount

	// Check if system can handle the total resource requirements
	memInfo, err := GetMemoryLimit()
	systemCPUs := runtime.NumCPU()

	if err == nil && memInfo.SystemMemory > 0 && totalMemory > int(float64(memInfo.SystemMemory)*0.8) {
		return fmt.Errorf("total memory for %d nodes (%dMB) exceeds 80%% of available system memory (%dMB)", nodeCount, totalMemory, memInfo.SystemMemory)
	}

	if totalCPUs > systemCPUs {
		return fmt.Errorf("total CPUs for %d nodes (%d) exceeds system CPUs (%d)", nodeCount, totalCPUs, systemCPUs)
	}

	// Validate HA configuration
	if cc.MultiNodeRequested && nodeCount < 2 {
		return errors.New("multi-node configuration requires at least 2 nodes")
	}

	return nil
}

// PreflightChecks performs quick validation before starting cluster creation
func (v *ResourceValidator) PreflightChecks(cc *config.ClusterConfig) []string {
	var warnings []string

	// Check for resource-intensive configurations
	if cc.Memory > 8192 {
		warnings = append(warnings, "High memory allocation may impact system performance")
	}

	if len(cc.Nodes) > 5 {
		warnings = append(warnings, "Large clusters may take significant time to provision")
	}

	if cc.Driver == "docker" && runtime.GOOS == "darwin" {
		warnings = append(warnings, "Docker Desktop on macOS may have resource limitations")
	}

	return warnings
}

