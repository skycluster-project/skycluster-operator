# ProviderProfile

Create a `providerprofile` resource for each provider you are intended to use. You need to specify the
`platform` and `region` of your provider such as `aws-us-east-1` along with selected zones.

Make sure the zones are available for your provider, for example:

**AWS CLI**:

```bash
 aws ec2 describe-availability-zones --region ca-central-1
```