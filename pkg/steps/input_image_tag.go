package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	imageapi "github.com/openshift/api/image/v1"
	"github.com/openshift/ci-operator/pkg/api"
	imageclientset "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	coreapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// inputImageTagStep will ensure that a tag exists
// in the pipeline ImageStream that resolves to
// the base image
type inputImageTagStep struct {
	config  api.InputImageTagStepConfiguration
	client  imageclientset.ImageStreamTagsGetter
	jobSpec *JobSpec

	imageName string
}

func (s *inputImageTagStep) Inputs(ctx context.Context, dry bool) (api.InputDefinition, error) {
	if len(s.imageName) > 0 {
		return api.InputDefinition{s.imageName}, nil
	}

	from, err := s.client.ImageStreamTags(s.config.BaseImage.Namespace).Get(fmt.Sprintf("%s:%s", s.config.BaseImage.Name, s.config.BaseImage.Tag), meta.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not resolve base image: %v", err)
	}
	log.Printf("Resolved %s/%s:%s to %s", s.config.BaseImage.Namespace, s.config.BaseImage.Name, s.config.BaseImage.Tag, from.Image.Name)
	s.imageName = from.Image.Name
	return api.InputDefinition{from.Image.Name}, nil
}

func (s *inputImageTagStep) Run(ctx context.Context, dry bool) error {
	log.Printf("Tagging %s/%s:%s into %s:%s", s.config.BaseImage.Namespace, s.config.BaseImage.Name, s.config.BaseImage.Tag, PipelineImageStream, s.config.To)

	_, err := s.Inputs(ctx, dry)
	if err != nil {
		return err
	}

	ist := &imageapi.ImageStreamTag{
		ObjectMeta: meta.ObjectMeta{
			Name:      fmt.Sprintf("%s:%s", PipelineImageStream, s.config.To),
			Namespace: s.jobSpec.Namespace(),
		},
		Tag: &imageapi.TagReference{
			ReferencePolicy: imageapi.TagReferencePolicy{
				Type: imageapi.LocalTagReferencePolicy,
			},
			From: &coreapi.ObjectReference{
				Kind:      "ImageStreamImage",
				Name:      fmt.Sprintf("%s@%s", s.config.BaseImage.Name, s.imageName),
				Namespace: s.config.BaseImage.Namespace,
			},
		},
	}
	if dry {
		istJSON, err := json.Marshal(ist)
		if err != nil {
			return fmt.Errorf("failed to marshal imagestreamtag: %v", err)
		}
		fmt.Printf("%s\n", istJSON)
		return nil
	}

	if _, err := s.client.ImageStreamTags(s.jobSpec.Namespace()).Create(ist); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (s *inputImageTagStep) Done() (bool, error) {
	log.Printf("Checking for existence of %s:%s", PipelineImageStream, s.config.To)
	_, err := s.client.ImageStreamTags(s.jobSpec.Namespace()).Get(
		fmt.Sprintf("%s:%s", PipelineImageStream, s.config.To),
		meta.GetOptions{},
	)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		} else {
			return false, err
		}
	} else {
		return true, nil
	}
}

func (s *inputImageTagStep) Requires() []api.StepLink {
	return []api.StepLink{api.ExternalImageLink(s.config.BaseImage)}
}

func (s *inputImageTagStep) Creates() []api.StepLink {
	return []api.StepLink{api.InternalImageLink(s.config.To)}
}

func (s *inputImageTagStep) Provides() (api.ParameterMap, api.StepLink) {
	return nil, nil
}

func (s *inputImageTagStep) Name() string { return "" }

func InputImageTagStep(config api.InputImageTagStepConfiguration, client imageclientset.ImageStreamTagsGetter, jobSpec *JobSpec) api.Step {
	return &inputImageTagStep{
		config:  config,
		client:  client,
		jobSpec: jobSpec,
	}
}
