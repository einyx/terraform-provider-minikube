//go:generate go run github.com/golang/mock/mockgen -source=$GOFILE -destination=mock_$GOFILE -package=$GOPACKAGE
package lib

import (
	"sync"

	"k8s.io/minikube/pkg/minikube/download"
)

type ParallelDownloader struct {
	MinikubeDownloader
}

func NewParallelDownloader() *ParallelDownloader {
	return &ParallelDownloader{
		MinikubeDownloader: MinikubeDownloader{},
	}
}

// DownloadAll downloads ISO and preload tarball in parallel
func (p *ParallelDownloader) DownloadAll(urls []string, skipChecksum bool, k8sVersion, containerRuntime, driver string) (string, error) {
	var (
		isoURL     string
		isoErr     error
		preloadErr error
		wg         sync.WaitGroup
	)

	wg.Add(2)

	// Download ISO in goroutine
	go func() {
		defer wg.Done()
		isoURL, isoErr = download.ISO(urls, skipChecksum)
	}()

	// Download preload tarball in goroutine
	go func() {
		defer wg.Done()
		preloadErr = download.Preload(k8sVersion, containerRuntime, driver)
	}()

	wg.Wait()

	// Check for errors
	if isoErr != nil {
		return "", isoErr
	}
	if preloadErr != nil {
		return "", preloadErr
	}

	return isoURL, nil
}

