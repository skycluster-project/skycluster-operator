package v1alpha1

const DefaultRefreshHourInstanceType = 12 // Default refresh interval in hours
const DefaultRefreshHourImages = 12       // Default refresh interval in hours

var ImageLabelList = []string{
	"ubuntu-20.04",
	"ubuntu-22.04",
	"ubuntu-24.04",
	"eksoptimized",
}

var ImageNameList = map[string]string{
	ImageLabelList[0]: "*ubuntu-focal-20.04*",
	ImageLabelList[1]: "*ubuntu-jammy-22.04*",
	ImageLabelList[2]: "*ubuntu-noble-24.04*",
	ImageLabelList[3]: "*amazon-eks-node-*",
}

var RegionToLocation = map[string]string{
	"us-east-1":      "US East (N. Virginia)",
	"us-east-2":      "US East (Ohio)",
	"us-west-1":      "US West (N. California)",
	"us-west-2":      "US West (Oregon)",
	"af-south-1":     "Africa (Cape Town)",
	"ap-east-1":      "Asia Pacific (Hong Kong)",
	"ap-south-1":     "Asia Pacific (Mumbai)",
	"ap-southeast-1": "Asia Pacific (Singapore)",
	"ap-southeast-2": "Asia Pacific (Sydney)",
	"ap-southeast-3": "Asia Pacific (Jakarta)",
	"ap-northeast-1": "Asia Pacific (Tokyo)",
	"ap-northeast-2": "Asia Pacific (Seoul)",
	"ap-northeast-3": "Asia Pacific (Osaka)",
	"ca-central-1":   "Canada (Central)",
	"eu-central-1":   "Europe (Frankfurt)",
	"eu-west-1":      "Europe (Ireland)",
	"eu-west-2":      "Europe (London)",
	"eu-west-3":      "Europe (Paris)",
	"eu-south-1":     "Europe (Milan)",
	"eu-north-1":     "Europe (Stockholm)",
	"me-south-1":     "Middle East (Bahrain)",
	"sa-east-1":      "South America (Sao Paulo)",
}

// Define base egress cost maps for AWS, Azure, GCP (example values, customize as needed)
var BaseCosts = map[string]map[string]string{
	"aws": {
		"zone":     "0.01", // $/GB intra zone
		"region":   "0.02", // $/GB intra region
		"internet": "0.09",
	},
	"azure": {
		"zone":     "0.005",
		"region":   "0.01",
		"internet": "0.087",
	},
	"gcp": {
		"zone":     "0.01",
		"region":   "0.015",
		"internet": "0.12",
	},
	"other": {
		"zone":     "0.02", // Default fallback costs
		"region":   "0.03",
		"internet": "0.15",
	},
}
