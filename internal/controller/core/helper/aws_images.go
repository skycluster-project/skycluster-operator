package helper

import (
	"context"
	"fmt"
	"sort"
	"time"

	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/samber/lo"

	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
)

func FetchAwsImage(ec2Client *ec2.Client, name string) ([]ec2Types.Image, error) {
	// check if name exists in the ImageList
	if !lo.Contains(corev1alpha1.ImageLabelList, name) {
		return nil, fmt.Errorf("image name %s not found in ImageList", name)
	}

	// "099720109477" // Ubuntu's official AWS account ID
	owner := lo.If(name == "eksoptimized", "602401143452").Else("099720109477")
	imageName := corev1alpha1.ImageNameList[name]
	images, err := FetchAwsImages(ec2Client, imageName, owner)
	if len(images) == 0 {
		return nil, fmt.Errorf("no Ubuntu images found: %v", err)
	}
	sortedImages := SortImagesByDate(images)
	if len(sortedImages) == 0 {
		return nil, fmt.Errorf("no sorted images found for name %s", name)
	}
	return sortedImages, nil
}

func FetchAwsUbuntuImagesAll(ec2Client *ec2.Client) (map[string][]ec2Types.Image, error) {
	// Fetches AWS images based on the provided name and owner.
	ubuntu2004, err0 := FetchAwsImages(ec2Client, "*ubuntu-focal-20.04*", "099720109477")
	ubuntu2204, err1 := FetchAwsImages(ec2Client, "*ubuntu-jammy-22.04*", "099720109477")
	ubuntu2404, err2 := FetchAwsImages(ec2Client, "*ubuntu-noble-24.04*", "099720109477")

	allImages := make(map[string][]ec2Types.Image)
	if err0 == nil {
		allImages["ubuntu-2004"] = ubuntu2004
	}
	if err1 == nil {
		allImages["ubuntu-2204"] = ubuntu2204
	}
	if err2 == nil {
		allImages["ubuntu-2404"] = ubuntu2404
	}
	if len(allImages) == 0 {
		return nil, fmt.Errorf("no Ubuntu images found: %v, %v, %v", err0, err1, err2)
	}
	return allImages, nil
}

func FetchAwsOptimizedImages(ec2Client *ec2.Client) ([]ec2Types.Image, error) {
	// Amazon EKS public AMI owner ID
	return FetchAwsImages(ec2Client, "*amazon-eks-node-*", "602401143452")
}

func FetchAwsImages(ec2Client *ec2.Client, name, owner string) ([]ec2Types.Image, error) {
	filters := []ec2Types.Filter{
		{
			Name:   aws.String("name"),
			Values: []string{name},
		},
		{
			Name:   aws.String("state"),
			Values: []string{"available"},
		},
		{
			Name:   aws.String("platform-details"),
			Values: []string{"Linux/UNIX"},
		},
		{
			Name:   aws.String("architecture"),
			Values: []string{"x86_64"},
		},
	}

	input := &ec2.DescribeImagesInput{
		Filters: filters,
		Owners:  []string{owner},
	}

	result, err := ec2Client.DescribeImages(context.TODO(), input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe images: %w", err)
	}

	return result.Images, nil
}

func SortImagesByDate(images []ec2Types.Image) []ec2Types.Image {
	sorted := make([]ec2Types.Image, len(images))
	copy(sorted, images)
	// Sort by creation date in descending order (most recent first)
	sort.Slice(sorted, func(i, j int) bool {
		t1, _ := time.Parse(time.RFC3339, *sorted[i].CreationDate)
		t2, _ := time.Parse(time.RFC3339, *sorted[j].CreationDate)
		return t1.After(t2)
	})
	return sorted
}

func MostRecentImage(images []ec2Types.Image) (ec2Types.Image, error) {
	if len(images) == 0 {
		return ec2Types.Image{}, errors.New("no images found")
	}

	var mostRecent ec2Types.Image
	latestTime := time.Time{}

	for _, img := range images {
		t, err := time.Parse(time.RFC3339, *img.CreationDate)
		if err != nil {
			continue
		}
		if t.After(latestTime) {
			latestTime = t
			mostRecent = img
		}
	}

	return mostRecent, nil
}
