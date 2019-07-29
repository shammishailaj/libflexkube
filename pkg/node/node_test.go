package node

import "testing"

func TestReadImageFail(t *testing.T) {
	node := &Node{}
	err := node.ReadImage()
	if err != nil && node.Image != "" {
		t.Errorf("If reading iamge failed, node image should be nil, got '%s'", node.Image)
	}
}

func TestReadImageOk(t *testing.T) {
	node := &Node{}
	err := node.ReadImage()
	if err == nil && node.Image == "" {
		t.Errorf("If reading image succeeded, node image should not be empty")
	}
}

func TestReadState(t *testing.T) {
	node := &Node{}
	if err := node.ReadState(); err != nil {
		t.Errorf("Reading node state should not fail")
	}
}

func TestReadStateSetImage(t *testing.T) {
	node := &Node{}
	err := node.ReadState()
	if err == nil && node.Image == "" {
		t.Errorf("Reading node state should read node image")
	}
}

func TestNodeNoName(t *testing.T) {
	node := &Node{}
	if err := node.Validate(); err == nil {
		t.Errorf("Node without name should not be a valid node")
	}
}

func TestNodeValidate(t *testing.T) {
	node := &Node{
		Name: "foo",
	}
	if err := node.Validate(); err != nil {
		t.Errorf("Node should be valid")
	}
}