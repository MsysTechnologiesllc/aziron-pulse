package k8s

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// portForwardEntry holds an active port-forward session.
type portForwardEntry struct {
	localPort uint16
	stopCh    chan struct{}
}

// PortForwardManager manages persistent kubectl-style port-forward connections
// to Kubernetes pods. It is intended for local development environments where
// the cluster's internal network (svc.cluster.local) and NodePort addresses
// are not directly reachable from the host (e.g. Docker-driver minikube on macOS).
//
// A single port-forward is re-used for all proxy requests to the same pod.
// Port-forwards are cleaned up when Stop() is called.
type PortForwardManager struct {
	mu        sync.RWMutex
	forwards  map[string]*portForwardEntry // key: "namespace/podName"
	config    *rest.Config
	clientset *kubernetes.Clientset
	logger    *zap.Logger
}

// NewPortForwardManager creates a new manager. Call Stop() on individual
// entries (or StopAll) when pods are deleted.
func NewPortForwardManager(config *rest.Config, clientset *kubernetes.Clientset, logger *zap.Logger) *PortForwardManager {
	return &PortForwardManager{
		forwards:  make(map[string]*portForwardEntry),
		config:    config,
		clientset: clientset,
		logger:    logger,
	}
}

// LocalPort returns the local port for an active port-forward to the given
// pod's remotePort (usually 8080). It creates a new port-forward if one does
// not exist yet.
func (m *PortForwardManager) LocalPort(namespace, podName string, remotePort int) (int, error) {
	key := namespace + "/" + podName

	m.mu.RLock()
	if entry, ok := m.forwards[key]; ok {
		m.mu.RUnlock()
		return int(entry.localPort), nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	// Re-check after upgrading to write lock.
	if entry, ok := m.forwards[key]; ok {
		return int(entry.localPort), nil
	}

	localPort, stopCh, err := m.start(namespace, podName, remotePort)
	if err != nil {
		return 0, err
	}
	m.forwards[key] = &portForwardEntry{localPort: localPort, stopCh: stopCh}
	m.logger.Info("Port-forward started",
		zap.String("pod", podName),
		zap.String("namespace", namespace),
		zap.Int("local_port", int(localPort)),
		zap.Int("remote_port", remotePort),
	)
	return int(localPort), nil
}

// Stop terminates the port-forward for the given pod (if any).
func (m *PortForwardManager) Stop(namespace, podName string) {
	key := namespace + "/" + podName
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.forwards[key]; ok {
		close(entry.stopCh)
		delete(m.forwards, key)
		m.logger.Info("Port-forward stopped", zap.String("pod", podName), zap.String("namespace", namespace))
	}
}

// StopAll terminates all active port-forwards.
func (m *PortForwardManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, entry := range m.forwards {
		close(entry.stopCh)
		delete(m.forwards, key)
		m.logger.Info("Port-forward stopped (StopAll)", zap.String("key", key))
	}
}

// start dials the Kubernetes API server and sets up a SPDY port-forward to
// the given pod. It returns the ephemeral local port that was assigned.
func (m *PortForwardManager) start(namespace, podName string, remotePort int) (uint16, chan struct{}, error) {
	roundTripper, upgrader, err := spdy.RoundTripperFor(m.config)
	if err != nil {
		return 0, nil, fmt.Errorf("portforward: spdy round-tripper: %w", err)
	}

	// Build the API-server portforward URL.
	apiHost := m.config.Host
	// The Host field may include a scheme (https://...). Strip it so we can
	// reconstruct a proper *url.URL.
	apiHost = strings.TrimPrefix(apiHost, "https://")
	apiHost = strings.TrimPrefix(apiHost, "http://")
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	serverURL := &url.URL{
		Scheme: "https",
		Host:   apiHost,
		Path:   path,
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, serverURL)

	// Use local port :0 so the OS assigns a free ephemeral port.
	ports := []string{fmt.Sprintf("0:%d", remotePort)}
	stopCh := make(chan struct{})
	readyCh := make(chan struct{})

	fw, err := portforward.New(dialer, ports, stopCh, readyCh, nil, nil)
	if err != nil {
		close(stopCh)
		return 0, nil, fmt.Errorf("portforward: create: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		if fwErr := fw.ForwardPorts(); fwErr != nil {
			errCh <- fwErr
		}
	}()

	// Wait until the port-forward is ready or fails.
	select {
	case <-readyCh:
	case err = <-errCh:
		return 0, nil, fmt.Errorf("portforward: forward ports: %w", err)
	}

	forwardedPorts, err := fw.GetPorts()
	if err != nil || len(forwardedPorts) == 0 {
		close(stopCh)
		return 0, nil, fmt.Errorf("portforward: get ports: %w", err)
	}

	return forwardedPorts[0].Local, stopCh, nil
}
