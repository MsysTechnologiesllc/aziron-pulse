package k8s

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Client wraps the Kubernetes client
type Client struct {
	Clientset        *kubernetes.Clientset
	MetricsClientset *metricsv.Clientset
	Config           *rest.Config
	Logger           *zap.Logger
}

// NewClient creates a new Kubernetes client
func NewClient(logger *zap.Logger) (*Client, error) {
	var config *rest.Config
	var err error

	// Try in-cluster config first
	config, err = rest.InClusterConfig()
	if err != nil {
		logger.Debug("Not running in cluster, trying kubeconfig")

		// Fall back to kubeconfig
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			home, _ := os.UserHomeDir()
			kubeconfig = filepath.Join(home, ".kube", "config")
		}

		logger.Info("Loading kubeconfig", zap.String("path", kubeconfig))

		// Use NewNonInteractiveDeferredLoadingClientConfig to properly load the kubeconfig
		// This handles certificate file paths correctly
		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

		config, err = kubeConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", kubeconfig, err)
		}

		logger.Info("Kubeconfig loaded successfully",
			zap.String("host", config.Host),
			zap.Bool("has_ca_data", len(config.CAData) > 0),
			zap.Bool("has_ca_file", config.CAFile != ""),
			zap.Bool("has_cert_data", len(config.CertData) > 0),
			zap.Bool("has_cert_file", config.CertFile != ""))
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create metrics clientset
	metricsClientset, err := metricsv.NewForConfig(config)
	if err != nil {
		logger.Warn("Failed to create metrics client", zap.Error(err))
		// Continue without metrics support
	}

	logger.Info("Kubernetes client created successfully")

	return &Client{
		Clientset:        clientset,
		MetricsClientset: metricsClientset,
		Config:           config,
		Logger:           logger,
	}, nil
}
