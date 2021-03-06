package api

import (
	"context"
	"fmt"
	"strings"
)

// Step is a self-contained bit of work that the
// build pipeline needs to do.
type Step interface {
	Inputs(ctx context.Context, dry bool) (InputDefinition, error)
	Run(ctx context.Context, dry bool) error
	Done() (bool, error)

	// Name is the name of the stage, used to target it.
	// If this is the empty string the stage cannot be targeted.
	Name() string
	Requires() []StepLink
	Creates() []StepLink
	Provides() (ParameterMap, StepLink)
}

type InputDefinition []string

type ParameterMap map[string]func() (string, error)

// StepLink abstracts the types of links that steps
// require and create.
type StepLink interface {
	Matches(other StepLink) bool
}

func ExternalImageLink(ref ImageStreamTagReference) StepLink {
	return &externalImageLink{image: ref}
}

type externalImageLink struct {
	image ImageStreamTagReference
}

func (l *externalImageLink) Matches(other StepLink) bool {
	switch link := other.(type) {
	case *externalImageLink:
		return l.image.Name == link.image.Name &&
			l.image.Namespace == link.image.Namespace &&
			l.image.Tag == link.image.Tag
	default:
		return false
	}
}

func InternalImageLink(ref PipelineImageStreamTagReference) StepLink {
	return &internalImageLink{image: ref}
}

type internalImageLink struct {
	image PipelineImageStreamTagReference
}

func (l *internalImageLink) Matches(other StepLink) bool {
	switch link := other.(type) {
	case *internalImageLink:
		return l.image == link.image
	default:
		return false
	}
}

func ImagesReadyLink() StepLink {
	return &imagesReadyLink{}
}

type imagesReadyLink struct{}

func (l *imagesReadyLink) Matches(other StepLink) bool {
	switch other.(type) {
	case *imagesReadyLink:
		return true
	default:
		return false
	}
}

func RPMRepoLink() StepLink {
	return &rpmRepoLink{}
}

type rpmRepoLink struct{}

func (l *rpmRepoLink) Matches(other StepLink) bool {
	switch other.(type) {
	case *rpmRepoLink:
		return true
	default:
		return false
	}
}

func ReleaseImagesLink() StepLink {
	return &releaseImagesLink{}
}

type releaseImagesLink struct{}

func (l *releaseImagesLink) Matches(other StepLink) bool {
	switch other.(type) {
	case *releaseImagesLink:
		return true
	default:
		return false
	}
}

type StepNode struct {
	Step     Step
	Children []*StepNode
}

// BuildGraph returns a graph or graphs that include
// all steps given.
func BuildGraph(steps []Step) []*StepNode {
	var allNodes []*StepNode
	for _, step := range steps {
		node := StepNode{Step: step, Children: []*StepNode{}}
		allNodes = append(allNodes, &node)
	}

	var roots []*StepNode
	for _, node := range allNodes {
		isRoot := true
		for _, other := range allNodes {
			for _, nodeRequires := range node.Step.Requires() {
				for _, otherCreates := range other.Step.Creates() {
					if nodeRequires.Matches(otherCreates) {
						isRoot = false
						addToNode(other, node)
					}
				}
			}
		}
		if isRoot {
			roots = append(roots, node)
		}
	}

	return roots
}

// BuildPartialGraph returns a graph or graphs that include
// only the dependencies of the named steps.
func BuildPartialGraph(steps []Step, names []string) ([]*StepNode, error) {
	if len(names) == 0 {
		return BuildGraph(steps), nil
	}

	var required []StepLink
	candidates := make([]bool, len(steps))
	for i, step := range steps {
		for j, name := range names {
			if name != step.Name() {
				continue
			}
			candidates[i] = true
			required = append(required, step.Requires()...)
			names = append(names[:j], names[j+1:]...)
			break
		}
	}
	if len(names) > 0 {
		return nil, fmt.Errorf("the following names were not found in the config or were duplicates: %s", strings.Join(names, ", "))
	}

	// identify all other steps that provide any links required by the current set
	for {
		added := 0
		for i, step := range steps {
			if candidates[i] {
				continue
			}
			if HasAnyLinks(required, step.Creates()) {
				added++
				candidates[i] = true
				required = append(required, step.Requires()...)
			}
		}
		if added == 0 {
			break
		}
	}

	var targeted []Step
	for i, candidate := range candidates {
		if candidate {
			targeted = append(targeted, steps[i])
		}
	}
	return BuildGraph(targeted), nil
}

func addToNode(parent, child *StepNode) bool {
	for _, s := range parent.Children {
		if s == child {
			return false
		}
	}
	parent.Children = append(parent.Children, child)
	return true
}

func HasAnyLinks(steps, candidates []StepLink) bool {
	for _, candidate := range candidates {
		for _, step := range steps {
			if step.Matches(candidate) {
				return true
			}
		}
	}
	return false
}
