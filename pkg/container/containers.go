package container

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"

	"github.com/flexkube/libflexkube/internal/util"
)

// ContainersInterface represents capabilities of containers struct.
type ContainersInterface interface {
	CheckCurrentState() error
	Execute() error
	CurrentStateToYaml() ([]byte, error)
	ToExported() *Containers
}

// Containers allow to orchestrate and update multiple containers spread
// across multiple hosts and update their configurations.
type Containers struct {
	// PreviousState stores previous state of the containers, which should be obtained and persisted
	// after containers modifications.
	PreviousState ContainersState `json:"previousState,omitempty"`
	// DesiredState is a user-defined desired containers configuration.
	DesiredState ContainersState `json:"desiredState,omitempty"`
}

// containers is a validated version of the Containers, which allows user to perform operations on them
// like planning, getting status etc.
type containers struct {
	// previousState is a previous state of the containers, given by user.
	previousState containersState
	// currentState stores current state of the containers. It is fed by calling Refresh() function.
	currentState containersState
	// resiredState is a user-defined desired containers configuration after validation.
	desiredState containersState
}

// New validates Containers configuration and returns "executable" containers object.
func (c *Containers) New() (ContainersInterface, error) {
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate containers configuration: %w", err)
	}

	// Validate already checks for errors, so we can skip checking here.
	previousState, _ := c.PreviousState.New()
	desiredState, _ := c.DesiredState.New()

	return &containers{
		previousState: previousState.(containersState),
		desiredState:  desiredState.(containersState),
	}, nil
}

// Validate validates Containers struct and all structs used underneath.
func (c *Containers) Validate() error {
	if c == nil || (c.PreviousState == nil && c.DesiredState == nil) {
		return fmt.Errorf("either current state or desired state must be defined")
	}

	previousState, err := c.PreviousState.New()
	if err != nil {
		return err
	}

	desiredState, err := c.DesiredState.New()
	if err != nil {
		return err
	}

	if len(previousState.(containersState)) == 0 && len(desiredState.(containersState)) == 0 {
		return fmt.Errorf("either current state or desired state should have containers defined")
	}

	return nil
}

// CheckCurrentState checks the state of existing containers and updates their state.
func (c *Containers) CheckCurrentState() error {
	containers, err := c.New()
	if err != nil {
		return err
	}

	if err := containers.CheckCurrentState(); err != nil {
		return err
	}

	*c = *containers.ToExported()

	return nil
}

// Deploy deploys defined containers.
func (c *Containers) Deploy() error {
	containers, err := c.New()
	if err != nil {
		return err
	}

	// TODO Deploy shouldn't refresh the state. However, due to how we handle exported/unexported
	// structs to enforce validation of objects, we lose current state, as we want it to be computed.
	// On the other hand, maybe it's a good thing to call it once we execute. This way we could compare
	// the plan user agreed to execute with plan calculated right before the execution and fail early if they
	// differ.
	// This is similar to what terraform is doing and may cause planning to run several times, so it may require
	// some optimization.
	// Alternatively we can have serializable plan and a knob in execute command to control whether we should
	// make additional validation or not.
	if err := containers.CheckCurrentState(); err != nil {
		return err
	}

	if err := containers.Execute(); err != nil {
		return err
	}

	*c = *containers.ToExported()

	return nil
}

// CheckCurrentState copies previous state to current state, to mark, that it has been called at least once
// and then updates state of all containers.
func (c *containers) CheckCurrentState() error {
	if c.currentState == nil {
		// We just assign the pointer, but it's fine, since we don't need previous
		// state anyway.
		// TODO we could keep previous state to inform user, that some external changes happened since last run
		c.currentState = c.previousState
	}

	return c.currentState.CheckState()
}

// filesToUpdate returns list of files, which needs to be updated, based on the current state of the container.
// If the file is missing or it's content is not the same as desired content, it will be added to the list.
func filesToUpdate(d hostConfiguredContainer, c *hostConfiguredContainer) []string {
	// If current state does not exist, just return all files.
	if c == nil {
		return util.KeysStringMap(d.configFiles)
	}

	files := []string{}

	// Loop over desired config files and check if they exist.
	for p, content := range d.configFiles {
		if currentContent, exists := c.configFiles[p]; !exists || content != currentContent {
			// TODO convert all prints to logging, so we can add more verbose information too
			fmt.Printf("Detected configuration drift for file '%s'\n", p)
			fmt.Printf("  current: \n%+v\n", currentContent)
			fmt.Printf("  desired: \n%+v\n", content)

			files = append(files, p)
		}
	}

	return files
}

// ensureConfigured makes sure that all desired configuration files are correct.
func (c *containers) ensureConfigured(n string) error {
	d := c.desiredState[n]

	// Container won't be needed anyway, so skip everything.
	if d == nil {
		return nil
	}

	r := c.currentState[n]

	if err := d.Configure(filesToUpdate(*d, r)); err != nil {
		return fmt.Errorf("failed creating config files: %w", err)
	}

	// If current state does not exist, simply replace it with desired state.
	if r == nil {
		c.currentState[n] = d
		r = d
	}

	// Update current state config files map.
	r.configFiles = d.configFiles

	return nil
}

// ensureRunning makes sure that given container is running.
func ensureRunning(c *hostConfiguredContainer) error {
	if c == nil {
		return fmt.Errorf("can't start non-existing container")
	}

	if c.container.IsRunning() {
		return nil
	}

	return c.Start()
}

func (c *containers) ensureExists(n string) error {
	r := c.currentState[n]
	if r != nil && r.container.Exists() {
		return nil
	}

	fmt.Printf("Creating new container '%s'\n", n)

	if err := c.desiredState.CreateAndStart(n); err != nil {
		return fmt.Errorf("failed creating new container: %w", err)
	}

	d := c.desiredState[n]

	// If current state does not exist, simply replace it with desired state.
	if r == nil {
		c.currentState[n] = d
		r = d
	}

	// After new container is created, add it to current state, so it can be returned to the user.
	r.container.Status = d.container.Status

	return nil
}

// isUpdatable determines if given container can be updated.
func (c *containers) isUpdatable(n string) error {
	// Container which currently does not exist can't be updated, only created.
	if _, ok := c.currentState[n]; !ok {
		return fmt.Errorf("can't update non-existing container '%s'", n)
	}

	// Container which is suppose to be removed shouldn't be updated.
	if _, ok := c.desiredState[n]; !ok {
		return fmt.Errorf("can't update container '%s', which is scheduler for removal", n)
	}

	return nil
}

// diffHost compares host fields of the container and returns it's diff.
//
// If the container cannot be updated, error is returned.
func (c *containers) diffHost(n string) (string, error) {
	if err := c.isUpdatable(n); err != nil {
		return "", fmt.Errorf("can't diff container: %w", err)
	}

	return cmp.Diff(c.currentState[n].host, c.desiredState[n].host), nil
}

// recreate is a helper, which removes container from current state and creates new one from
// desired state.
func (c *containers) recreate(n string) error {
	if err := c.currentState.RemoveContainer(n); err != nil {
		return fmt.Errorf("failed removing old container: %w", err)
	}

	return c.desiredState.CreateAndStart(n)
}

// ensureHost makes sure container is running on the right host.
//
// If host configuration changes, existing container will be removed and new one will be created.
//
// TODO This might be an overkill. e.g. changing SSH key for deployment will re-create all containers.
func (c *containers) ensureHost(n string) error {
	diff, err := c.diffHost(n)
	if err != nil {
		return fmt.Errorf("failed to check host diff: %w", err)
	}

	if diff == "" {
		return nil
	}

	fmt.Printf("Detected host configuration drift '%s'\n", n)
	fmt.Printf("  Diff: %v\n", diff)

	if err := c.recreate(n); err != nil {
		return fmt.Errorf("failed updating container: %w", err)
	}

	// After new container is created, add it to current state, so it can be returned to the user.
	c.currentState[n] = c.desiredState[n]

	return nil
}

// diffContainer compares container fields of the container and returns it's diff.
//
// If the container cannot be updated, error is returned.
func (c *containers) diffContainer(n string) (string, error) {
	if err := c.isUpdatable(n); err != nil {
		return "", fmt.Errorf("can't diff container: %w", err)
	}

	return cmp.Diff(c.currentState[n].container.Config, c.desiredState[n].container.Config), nil
}

// ensureContainer makes sure container configuration is up to date.
//
// If container configuration changes, existing container will be removed and new one will be created.
func (c *containers) ensureContainer(n string) error {
	diff, err := c.diffContainer(n)
	if err != nil {
		return fmt.Errorf("failed to check container diff: %w", err)
	}

	if diff == "" {
		return nil
	}

	fmt.Printf("Detected container configuration drift '%s'\n", n)
	fmt.Printf("  Diff: %v\n", diff)

	if err := c.recreate(n); err != nil {
		return fmt.Errorf("failed updating container: %w", err)
	}

	// After new container is created, add it to current state, so it can be returned to the user.
	c.currentState[n] = c.desiredState[n]

	return nil
}

// Execute checks for containers configuration drifts and tries to reach desired state.
//
// TODO we should break down this function into smaller functions
// TODO add planning, so it is possible to inspect what will be done
// TODO currently we only compare previous configuration with new configuration.
// We should also read runtime parameters and confirm that everything is according
// to the spec.
func (c *containers) Execute() error {
	if c.currentState == nil {
		return fmt.Errorf("can't execute without knowing current state of the containers")
	}

	fmt.Println("Checking for stopped and missing containers")

	for n, r := range c.currentState {
		// Container is gone, we need to re-create it.
		if !r.container.Exists() {
			delete(c.currentState, n)
			continue
		}

		if err := ensureRunning(r); err != nil {
			return fmt.Errorf("failed to start stopped container: %w", err)
		}
	}

	fmt.Println("Configuring and creating new containers")

	for i := range c.desiredState {
		if err := c.ensureConfigured(i); err != nil {
			return fmt.Errorf("failed configuring container %s: %w", i, err)
		}

		if err := c.ensureExists(i); err != nil {
			return fmt.Errorf("failed creating new container %s: %w", i, err)
		}
	}

	fmt.Println("Updating existing containers")

	for i := range c.currentState {
		// Update containers on hosts.
		// This can move containers between hosts, but NOT the data.
		if err := c.ensureHost(i); err != nil {
			return fmt.Errorf("failed updating host configuration of container %s: %w", i, err)
		}

		if err := c.ensureConfigured(i); err != nil {
			return fmt.Errorf("failed updating configuration for container %s: %w", i, err)
		}

		if err := c.ensureContainer(i); err != nil {
			return fmt.Errorf("failed updating container %s: %w", i, err)
		}

		// If container is not scheduled for removal, move to the next one.
		if _, exists := c.desiredState[i]; exists {
			continue
		}

		if err := c.currentState.RemoveContainer(i); err != nil {
			return fmt.Errorf("failed removing old container: %w", err)
		}
	}

	return nil
}

// FromYaml allows to restore containers state from YAML.
func FromYaml(c []byte) (ContainersInterface, error) {
	containers := &Containers{}
	if err := yaml.Unmarshal(c, &containers); err != nil {
		return nil, fmt.Errorf("failed to parse input yaml: %w", err)
	}

	cl, err := containers.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create containers object: %w", err)
	}

	return cl, nil
}

// CurrentStateToYaml dumps current state as previousState in exported format,
// which can be serialized and stored.
func (c *containers) CurrentStateToYaml() ([]byte, error) {
	containers := &Containers{
		PreviousState: c.previousState.Export(),
	}

	return yaml.Marshal(containers)
}

// ToExported converts containers struct to exported Containers.
func (c *containers) ToExported() *Containers {
	return &Containers{
		PreviousState: c.previousState.Export(),
		DesiredState:  c.desiredState.Export(),
	}
}
