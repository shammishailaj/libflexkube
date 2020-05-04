package pki

import (
	"fmt"
)

const (
	// KubernetesCACN is a default CN for Kubernetes CA certificate, as recommended by
	// https://kubernetes.io/docs/setup/best-practices/certificates/.
	KubernetesCACN = "kubernetes-ca"

	// KubernetesFrontProxyCACN is a default CN for Kubernetes front proxy CA certificate,
	// as recommended by https://kubernetes.io/docs/setup/best-practices/certificates/.
	KubernetesFrontProxyCACN = "kubernetes-front-proxy-ca"
)

// Kubernetes stores Kubernetes PKI and settings.
type Kubernetes struct {
	// Inline Certificate struct, so some settings can be applied as defaults for all Kubernetes certificates.
	Certificate

	CA *Certificate `json:"ca,omitempty"`

	FrontProxyCA *Certificate `json:"frontProxyCA,omitempty"`

	KubeAPIServer                    *KubeAPIServer `json:"kubeAPIServer,omitempty"`
	AdminCertificate                 *Certificate   `json:"adminCertificate,omitempty"`
	KubeControllerManagerCertificate *Certificate   `json:"kubeControllerManagerCertificate,omitempty"`
	KubeSchedulerCertificate         *Certificate   `json:"kubeSchedulerCertificate,omitempty"`
	ServiceAccountCertificate        *Certificate   `json:"serviceAccountCertificate,omitempty"`
}

// KubeAPIServer stores kube-apiserver certificates.
type KubeAPIServer struct {
	Certificate

	ExternalNames []string `json:"externalNames,omitempty"`
	ServerIPs     []string `json:"serverIPs,omitempty"`
	ClusterDomain string   `json:"clusterDomain,omitempty"`

	ServerCertificate           *Certificate `json:"serverCertificate,omitempty"`
	KubeletCertificate          *Certificate `json:"kubeletCertificate,omitempty"`
	FrontProxyClientCertificate *Certificate `json:"frontProxyClientCertificate,omitempty"`
}

func (k *Kubernetes) kubernetesCACR(rootCA *Certificate, defaultCertificate Certificate) *certificateRequest {
	if k.CA == nil {
		k.CA = &Certificate{}
	}

	return &certificateRequest{
		Target: k.CA,
		CA:     rootCA,
		Certificates: []*Certificate{
			&defaultCertificate,
			&k.Certificate,
			caCertificate(KubernetesCACN),
			k.CA,
		},
	}
}

func (k *Kubernetes) kubernetesFrontProxyCACR(rootCA *Certificate, defaultCertificate Certificate) *certificateRequest {
	if k.FrontProxyCA == nil {
		k.FrontProxyCA = &Certificate{}
	}

	return &certificateRequest{
		Target: k.FrontProxyCA,
		CA:     rootCA,
		Certificates: []*Certificate{
			&defaultCertificate,
			&k.Certificate,
			caCertificate(KubernetesFrontProxyCACN),
			k.FrontProxyCA,
		},
	}
}

func (k *Kubernetes) kubeAPIServerServerCR(defaultCertificate Certificate) *certificateRequest {
	if k.KubeAPIServer.ServerCertificate == nil {
		k.KubeAPIServer.ServerCertificate = &Certificate{}
	}

	return &certificateRequest{
		Target: k.KubeAPIServer.ServerCertificate,
		CA:     k.CA,
		Certificates: []*Certificate{
			&defaultCertificate,
			&k.Certificate,
			&k.KubeAPIServer.Certificate,
			defaultKubeAPIServerServerCertificate(k.KubeAPIServer),
			k.KubeAPIServer.ServerCertificate,
		},
	}
}

func (k *Kubernetes) kubeAPIServerKubeletCR(defaultCertificate Certificate) *certificateRequest {
	if k.KubeAPIServer.KubeletCertificate == nil {
		k.KubeAPIServer.KubeletCertificate = &Certificate{}
	}

	return &certificateRequest{
		Target: k.KubeAPIServer.KubeletCertificate,
		CA:     k.CA,
		Certificates: []*Certificate{
			&defaultCertificate,
			&k.Certificate,
			&k.KubeAPIServer.Certificate,
			defaultKubeAPIServerKubeletCertificate(),
			k.KubeAPIServer.KubeletCertificate,
		},
	}
}

func (k *Kubernetes) kubeAPIServerFrontProxyClientCR(defaultCertificate Certificate) *certificateRequest {
	if k.KubeAPIServer.FrontProxyClientCertificate == nil {
		k.KubeAPIServer.FrontProxyClientCertificate = &Certificate{}
	}

	return &certificateRequest{
		Target: k.KubeAPIServer.FrontProxyClientCertificate,
		CA:     k.FrontProxyCA,
		Certificates: []*Certificate{
			&defaultCertificate,
			&k.Certificate,
			&k.KubeAPIServer.Certificate,
			defaultKubeAPIServerFrontProxyClientCertificate(),
			k.KubeAPIServer.FrontProxyClientCertificate,
		},
	}
}

func (k *Kubernetes) adminCR(defaultCertificate Certificate) *certificateRequest {
	if k.AdminCertificate == nil {
		k.AdminCertificate = &Certificate{}
	}

	return &certificateRequest{
		Target: k.AdminCertificate,
		CA:     k.CA,
		Certificates: []*Certificate{
			&defaultCertificate,
			&k.Certificate,
			{
				CommonName:   "kubernetes-admin",
				Organization: "system:masters",
				KeyUsage:     clientUsage(),
			},
			k.AdminCertificate,
		},
	}
}

func (k *Kubernetes) kubeControllerManagerCR(defaultCertificate Certificate) *certificateRequest {
	if k.KubeControllerManagerCertificate == nil {
		k.KubeControllerManagerCertificate = &Certificate{}
	}

	return &certificateRequest{
		Target: k.KubeControllerManagerCertificate,
		CA:     k.CA,
		Certificates: []*Certificate{
			&defaultCertificate,
			&k.Certificate,
			{
				CommonName: "system:kube-controller-manager",
				KeyUsage:   clientUsage(),
			},
			k.KubeControllerManagerCertificate,
		},
	}
}

// Generate generates Kubernetes PKI.
func (k *Kubernetes) Generate(rootCA *Certificate, defaultCertificate Certificate) error {
	crs := []*certificateRequest{
		k.kubernetesCACR(rootCA, defaultCertificate),
		k.kubernetesFrontProxyCACR(rootCA, defaultCertificate),
	}

	if err := buildAndGenerate(crs...); err != nil {
		return fmt.Errorf("failed to generate kubernetes CA certificates: %w", err)
	}

	if k.KubeAPIServer == nil {
		k.KubeAPIServer = &KubeAPIServer{}
	}

	crs = []*certificateRequest{
		k.kubeAPIServerServerCR(defaultCertificate),
		k.kubeAPIServerKubeletCR(defaultCertificate),
		k.kubeAPIServerFrontProxyClientCR(defaultCertificate),
		k.adminCR(defaultCertificate),
		k.kubeControllerManagerCR(defaultCertificate),
		k.kubeSchedulerCR(defaultCertificate),
		k.serviceAccountCR(defaultCertificate),
	}

	return buildAndGenerate(crs...)
}

func (k *Kubernetes) serviceAccountCR(defaultCertificate Certificate) *certificateRequest {
	if k.ServiceAccountCertificate == nil {
		k.ServiceAccountCertificate = &Certificate{}
	}

	return &certificateRequest{
		Target: k.ServiceAccountCertificate,
		CA:     k.CA,
		Certificates: []*Certificate{
			&defaultCertificate,
			&k.Certificate,
			k.ServiceAccountCertificate,
		},
	}
}

func (k *Kubernetes) kubeSchedulerCR(defaultCertificate Certificate) *certificateRequest {
	if k.KubeSchedulerCertificate == nil {
		k.KubeSchedulerCertificate = &Certificate{}
	}

	return &certificateRequest{
		Target: k.KubeSchedulerCertificate,
		CA:     k.CA,
		Certificates: []*Certificate{
			&defaultCertificate,
			&k.Certificate,
			{
				CommonName: "system:kube-scheduler",
				KeyUsage:   clientUsage(),
			},
			k.KubeSchedulerCertificate,
		},
	}
}

func defaultKubeAPIServerServerCertificate(k *KubeAPIServer) *Certificate {
	c := &Certificate{
		CommonName:  "kube-apiserver",
		IPAddresses: []string{"127.0.0.1"},
		DNSNames: []string{
			"localhost",
			// Recommended by TLS certificates guide.
			"kubernetes",
			"kubernetes.default",
			"kubernetes.default.svc",
			"kubernetes.default.svc.cluster",
			"kubernetes.default.svc.cluster.local",
		},
		KeyUsage: serverUsage(),
	}

	if k != nil {
		c.DNSNames = append(c.DNSNames, k.ExternalNames...)
		c.IPAddresses = append(c.IPAddresses, k.ServerIPs...)
	}

	return c
}

func defaultKubeAPIServerKubeletCertificate() *Certificate {
	return &Certificate{
		CommonName:   "kube-apiserver-kubelet-client",
		Organization: "system:masters",
		KeyUsage:     clientUsage(),
	}
}

func defaultKubeAPIServerFrontProxyClientCertificate() *Certificate {
	return &Certificate{
		CommonName: "front-proxy-client",
		KeyUsage:   clientUsage(),
	}
}
