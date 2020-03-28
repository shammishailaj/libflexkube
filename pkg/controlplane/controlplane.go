// Package controlplane allows to create and manage static Kubernetes controlplane running
// in containers.
package controlplane

import (
	"fmt"

	"sigs.k8s.io/yaml"

	"github.com/flexkube/libflexkube/internal/util"
	"github.com/flexkube/libflexkube/pkg/container"
	"github.com/flexkube/libflexkube/pkg/defaults"
	"github.com/flexkube/libflexkube/pkg/host"
	"github.com/flexkube/libflexkube/pkg/host/transport/ssh"
	"github.com/flexkube/libflexkube/pkg/kubernetes/client"
	"github.com/flexkube/libflexkube/pkg/types"
)

// Common struct contains fields, which are common between all controlplane components.
type Common struct {
	Image                   string            `json:"image,omitempty"`
	KubernetesCACertificate types.Certificate `json:"kubernetesCACertificate,omitempty"`
	FrontProxyCACertificate types.Certificate `json:"frontProxyCACertificate,omitempty"`
}

// GetImage returns either image defined in common config or Kubernetes default image.
func (co Common) GetImage() string {
	return util.PickString(co.Image, defaults.KubernetesImage)
}

// Controlplane represents kubernetes controlplane configuration and state from the user.
type Controlplane struct {
	// User-configurable fields.
	// They should be defined here if they are used more than once. Things like serviceCIDR, which is only needed in KubeAPIServer,
	// should be defined directly there.
	Common                *Common               `json:"common,omitempty"`
	SSH                   *ssh.Config           `json:"ssh,omitempty"`
	APIServerAddress      string                `json:"apiServerAddress,omitempty"`
	APIServerPort         int                   `json:"apiServerPort,omitempty"`
	KubeAPIServer         KubeAPIServer         `json:"kubeAPIServer,omitempty"`
	KubeControllerManager KubeControllerManager `json:"kubeControllerManager,omitempty"`
	KubeScheduler         KubeScheduler         `json:"kubeScheduler,omitempty"`
	Hosts                 []host.Host           `json:"hosts,omitempty"`

	// Serializable fields.
	State *container.ContainersState `json:"state,omitempty"`
}

// controlplane is executable version of Controlplane, with validated fields and calculated containers.
type controlplane struct {
	containers container.ContainersInterface
}

// propagateKubeconfig merges given client config with values stored in Controlplane.
// Values in given config has priority over ones from the Controlplane.
func (c *Controlplane) propagateKubeconfig(d *client.Config) {
	d.CACertificate = types.Certificate(util.PickString(string(d.CACertificate), string(c.Common.KubernetesCACertificate)))

	if c.APIServerAddress != "" && c.APIServerPort != 0 {
		d.Server = util.PickString(d.Server, fmt.Sprintf("%s:%d", c.APIServerAddress, c.APIServerPort))
	}
}

// propagateHost merges given host configuration with values stored in Controlplane.
// Values in given host config has priority over ones from the Controlplane.
func (c *Controlplane) propagateHost(h *host.Host) *host.Host {
	if h == nil {
		h = &host.Host{}
	}

	nh := host.BuildConfig(*h, host.Host{
		SSHConfig: c.SSH,
	})

	return &nh
}

// propagateCommon merges given common configuration with values stored in Controlplane.
// Values in given common configuration has priority over ones from the Controlplane.
func (c *Controlplane) propagateCommon(co *Common) {
	if co == nil {
		co = &Common{}
	}

	if c.Common == nil {
		c.Common = &Common{}
	}

	co.Image = util.PickString(co.Image, c.Common.Image)
	co.KubernetesCACertificate = types.Certificate(util.PickString(string(co.KubernetesCACertificate), string(c.Common.KubernetesCACertificate)))
	co.FrontProxyCACertificate = types.Certificate(util.PickString(string(co.FrontProxyCACertificate), string(c.Common.FrontProxyCACertificate)))
}

// buildKubeScheduler fills KubeSheduler struct with all default values.
func (c *Controlplane) buildKubeScheduler() {
	k := &c.KubeScheduler

	c.propagateKubeconfig(&k.Kubeconfig)

	c.propagateCommon(k.Common)

	k.Host = c.propagateHost(k.Host)
}

// buildKubeControllerManager fills KubeControllerManager with all default values.
func (c *Controlplane) buildKubeControllerManager() {
	k := &c.KubeControllerManager

	c.propagateKubeconfig(&k.Kubeconfig)

	c.propagateCommon(k.Common)

	k.Host = c.propagateHost(k.Host)
}

// buildKubeAPIServer fills KubeAPIServer with all default values.
func (c *Controlplane) buildKubeAPIServer() {
	k := &c.KubeAPIServer

	if k.BindAddress == "" && c.APIServerAddress != "" {
		k.BindAddress = c.APIServerAddress
	}

	if k.AdvertiseAddress == "" && c.APIServerAddress != "" {
		k.AdvertiseAddress = c.APIServerAddress
	}

	if k.SecurePort == 0 && c.APIServerPort != 0 {
		k.SecurePort = c.APIServerPort
	}

	c.propagateCommon(k.Common)

	k.Host = c.propagateHost(k.Host)
}

// New validates Controlplane configuration and fills populates all values provided by the users
// to the structs underneath.
func (c *Controlplane) New() (types.Resource, error) {
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate controlplane configuration: %w", err)
	}

	s := c.State
	if s == nil {
		s = &container.ContainersState{}
	}

	cc := &container.Containers{
		PreviousState: *s,
	}

	co, _ := cc.New()

	controlplane := &controlplane{
		containers: co,
	}

	// Make sure all values are filled.
	c.buildComponents()

	// Skip error checking, as it's done in Verify().
	kas, _ := c.KubeAPIServer.New()
	kasHcc, _ := kas.ToHostConfiguredContainer()

	kcm, _ := c.KubeControllerManager.New()
	kcmHcc, _ := kcm.ToHostConfiguredContainer()

	ks, _ := c.KubeScheduler.New()
	ksHcc, _ := ks.ToHostConfiguredContainer()

	cc.DesiredState = container.ContainersState{
		"kube-apiserver":          kasHcc,
		"kube-controller-manager": kcmHcc,
		"kube-scheduler":          ksHcc,
	}

	co, _ = cc.New()

	controlplane.containers = co

	return controlplane, nil
}

// buildComponents fills controlplane component structs with default values inherited
// from controlplane struct.
func (c *Controlplane) buildComponents() {
	c.buildKubeAPIServer()
	c.buildKubeControllerManager()
	c.buildKubeScheduler()
}

// Validate validates Controlplane configuration.
func (c *Controlplane) Validate() error {
	c.buildComponents()

	var errors util.ValidateError

	kas, err := c.KubeAPIServer.New()
	if err != nil {
		errors = append(errors, fmt.Errorf("failed to verify kube-apiserver configuration: %w", err))
	}

	kcm, err := c.KubeControllerManager.New()
	if err != nil {
		errors = append(errors, fmt.Errorf("failed to verify kube-controller-manager: %w", err))
	}

	ks, err := c.KubeScheduler.New()
	if err != nil {
		errors = append(errors, fmt.Errorf("failed to verify kube-scheduler configuration: %w", err))
	}

	// If there were any errors while creating objects, it's not safe to proceed.
	if len(errors) > 0 {
		return errors.Return()
	}

	if _, err := kas.ToHostConfiguredContainer(); err != nil {
		errors = append(errors, fmt.Errorf("failed to build kube-apiserver container configuration: %w", err))
	}

	if _, err := kcm.ToHostConfiguredContainer(); err != nil {
		errors = append(errors, fmt.Errorf("failed to build kube-controller-manager container configuration: %w", err))
	}

	if _, err := ks.ToHostConfiguredContainer(); err != nil {
		errors = append(errors, fmt.Errorf("failed to build kube-scheduler container configuration: %w", err))
	}

	return errors.Return()
}

// FromYaml allows to restore controlplane state from YAML.
func FromYaml(c []byte) (types.Resource, error) {
	return types.ResourceFromYaml(c, &Controlplane{})
}

// StateToYaml allows to dump controlplane state to YAML, so it can be restored later.
func (c *controlplane) StateToYaml() ([]byte, error) {
	return yaml.Marshal(Controlplane{State: &c.containers.ToExported().PreviousState})
}

func (c *controlplane) CheckCurrentState() error {
	return c.containers.CheckCurrentState()
}

// Deploy checks the status of the control plane and deploys configuration updates.
func (c *controlplane) Deploy() error {
	return c.containers.Deploy()
}

// Containers implement types.Resource interface.
func (c *controlplane) Containers() container.ContainersInterface {
	return c.containers
}
