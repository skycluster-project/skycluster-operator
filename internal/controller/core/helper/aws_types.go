package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	lo "github.com/samber/lo"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	pricing "github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
)

func FetchAwsInstanceTypes(client *ec2.Client, zoneName string) ([]ec2Types.InstanceTypeOffering, error) {
	// DescribeInstanceTypeOfferings returns a list of instance types available in the specified availability zone.
	// The LocationType is set to "availability-zone" to filter by the specific zone.
	input := &ec2.DescribeInstanceTypeOfferingsInput{
		LocationType: "availability-zone",
		Filters: []ec2Types.Filter{
			{
				Name:   aws.String("location"),
				Values: []string{zoneName},
			},
			{
				Name:   aws.String("instance-type"),
				Values: []string{"t3*", "t4*", "m5*", "m6*", "g4*", "g5*", "p3*", "p4*"},
			},
		},
	}

	output, err := client.DescribeInstanceTypeOfferings(context.TODO(), input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe instance type offerings: %w", err)
	}

	return output.InstanceTypeOfferings, nil
}

// fetchInstanceTypes returns EC2 instance types with vCPUs and RAM.
func FetchAwsInstanceTypeDetials(client *ec2.Client, pricingClient *pricing.Client, instanceType string) (*corev1alpha1.InstanceOffering, error) {
	flavors := []corev1alpha1.InstanceOffering{}
	itsOut, err := client.DescribeInstanceTypes(context.TODO(), &ec2.DescribeInstanceTypesInput{
		Filters: []ec2Types.Filter{
			{
				Name:   aws.String("instance-type"),
				Values: []string{instanceType},
			},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to describe instance types: %w", err)
	}
	if len(itsOut.InstanceTypes) == 0 {
		return nil, nil
	}

	for _, it := range itsOut.InstanceTypes {
		vcpus := it.VCpuInfo.DefaultVCpus
		ramMb := it.MemoryInfo.SizeInMiB
		flavorName := string(it.InstanceType)
		ramGB := float64(*ramMb) / 1024.0
		flavorLabel := fmt.Sprintf("%dvCPU-%dGB", int(aws.ToInt32(vcpus)), int(ramGB))

		var gpuCount, gpuMemory int32 = 0, 0
		var gpuManufacturer, gpuModel string

		if it.GpuInfo != nil && len(it.GpuInfo.Gpus) > 0 {
			gpu := it.GpuInfo.Gpus[0]
			gpuCount = *gpu.Count
			gpuManufacturer = aws.ToString(gpu.Manufacturer)
			gpuModel = aws.ToString(gpu.Name)
			gpuMemory = aws.ToInt32(gpu.MemoryInfo.SizeInMiB)
		}
		// we expect one instance type per call, so we can directly append
		// Pricing API only works with region us-east-1 for now
		// We use the price from us-east-1 as a reference for all other regions
		onDemandPrice, _ := fetchFlavorOnDemandPirce(pricingClient, "us-east-1", flavorName)
		flavors = append(flavors, corev1alpha1.InstanceOffering{
			NameLabel: flavorLabel,
			Name:      flavorName,
			VCPUs:     int(aws.ToInt32(vcpus)),
			Price:     lo.If(onDemandPrice != "", onDemandPrice).Else("-1"),
			RAM:       fmt.Sprintf("%dGB", int(ramGB)),
			Spot: corev1alpha1.Spot{
				Enabled: false, // Spot prices are fetched separately
				Price:   "-1",  // Placeholder, will be updated later
			},
			GPU: corev1alpha1.GPU{
				Enabled:      gpuCount > 0,
				Count:        int(gpuCount),
				Manufacturer: gpuManufacturer,
				Model:        gpuModel,
				Memory:       fmt.Sprintf("%dGB", gpuCount*int32(gpuMemory/1024)),
			},
		})
		break // We only need one instance type per call
	}

	// We expect one instance type per call, so we can directly append
	return &flavors[0], nil
}

func FetchAwsSpotInstanceTypes(client *ec2.Client, instanceTypes []string, zoneName string) (map[string]string, error) {
	spotPrice := make(map[string]string)

	// spotInstancePrices := make(map[string]string)
	for _, itype := range instanceTypes {
		input := &ec2.DescribeSpotPriceHistoryInput{
			InstanceTypes:       []ec2Types.InstanceType{ec2Types.InstanceType(itype)},
			AvailabilityZone:    aws.String(zoneName),
			ProductDescriptions: []string{"Linux/UNIX"},
			StartTime:           aws.Time(time.Now().Add(-1 * time.Hour)),
			EndTime:             aws.Time(time.Now()),
			MaxResults:          aws.Int32(1),
		}

		output, err := client.DescribeSpotPriceHistory(context.TODO(), input)
		if err != nil {
			return nil, fmt.Errorf("failed to describe spot price history for %s: %w", itype, err)
		}

		if len(output.SpotPriceHistory) == 0 {
			fmt.Printf("No spot price found for %s in %s\n", itype, zoneName)
			continue
		}

		spotPrice[itype] = *output.SpotPriceHistory[0].SpotPrice
	}

	return spotPrice, nil
}

func fetchFlavorOnDemandPirce(client *pricing.Client, region string, flavorName string) (string, error) {
	serviceCode := "AmazonEC2"
	// fmt.Printf("Fetching on-demand price for flavor %s in region %s\n", flavorName, corev1alpha1.RegionToLocation[region])
	input := &pricing.GetProductsInput{
		ServiceCode: aws.String(serviceCode),
		Filters: []pricingTypes.Filter{
			{
				Type:  pricingTypes.FilterTypeTermMatch,
				Field: aws.String("instanceType"),
				Value: aws.String(flavorName),
			},
			{
				Type:  pricingTypes.FilterTypeTermMatch,
				Field: aws.String("location"),
				Value: aws.String(corev1alpha1.RegionToLocation[region]),
			},
			{
				Type:  pricingTypes.FilterTypeTermMatch,
				Field: aws.String("operatingSystem"),
				Value: aws.String("Linux"),
			},
			{
				Type:  pricingTypes.FilterTypeTermMatch,
				Field: aws.String("preInstalledSw"),
				Value: aws.String("NA"),
			},
			{
				Type:  pricingTypes.FilterTypeTermMatch,
				Field: aws.String("tenancy"),
				Value: aws.String("Shared"),
			},
			{
				Type:  pricingTypes.FilterTypeTermMatch,
				Field: aws.String("capacitystatus"),
				Value: aws.String("Used"),
			},
		},
		MaxResults: aws.Int32(1), // We only need one price per instance type
	}
	out, err := client.GetProducts(context.Background(), input)
	if err != nil {
		return "", err
	}
	if len(out.PriceList) == 0 {
		return "", fmt.Errorf("no price found for flavor %s in region %s", flavorName, region)
	}
	onDemandPrice, err := extractPriceFromJSON(out.PriceList[0])
	if err != nil {
		return "", err
	}
	return onDemandPrice, nil
}

func extractPriceFromJSON(raw string) (string, error) {
	// Minimal parsing ignoring error handling for the complex AWS price JSON,
	// here better use a full parser, but for brevity:
	//
	// The JSON has structure:
	// {
	//   "terms": {
	//     "OnDemand": {
	//       "<sku>": {
	//         "priceDimensions": {
	//           "<priceDimensionCode>": {
	//             "pricePerUnit": {
	//               "USD": "0.0416"
	//             },
	// ...

	type priceDimension struct {
		PricePerUnit map[string]string `json:"pricePerUnit"`
	}
	type termDetails struct {
		PriceDimensions map[string]priceDimension `json:"priceDimensions"`
	}
	type productTerms struct {
		OnDemand map[string]termDetails `json:"OnDemand"`
		Spot     map[string]termDetails `json:"Spot,omitempty"`
	}
	type productPriceData struct {
		Terms productTerms `json:"terms"`
	}

	var onDemandPrice string
	var onDemandFound bool
	jsonData := []byte(raw)

	var parsedData productPriceData
	if err := json.Unmarshal(jsonData, &parsedData); err != nil {
		return "-1", fmt.Errorf("failed to parse JSON: %v", err)
	}

	for _, term := range parsedData.Terms.OnDemand {
		for _, dimension := range term.PriceDimensions {
			if priceStr, ok := dimension.PricePerUnit["USD"]; ok {
				onDemandPrice = priceStr
				onDemandFound = true
				break
			}
		}
	}

	if !onDemandFound {
		return "-1", fmt.Errorf("no valid price found in JSON")
	}

	return onDemandPrice, nil
}
