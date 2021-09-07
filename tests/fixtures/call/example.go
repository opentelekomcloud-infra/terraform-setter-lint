package vpc

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func ResourceVirtualPrivateCloudV1() *schema.Resource {
	return &schema.Resource{
		Schema: map[string]*schema.Schema{ // request and response parameters
			"tags": common.TagsSchema(),
		},
	}
}
