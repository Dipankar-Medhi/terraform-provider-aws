package aws

import (
	"fmt"
	"log"
	"regexp"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/hashicorp/go-multierror"
	sdkacctest "github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-provider-aws/internal/acctest"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/provider"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/verify"
)

func init() {
	resource.AddTestSweepers("aws_iam_policy", &resource.Sweeper{
		Name: "aws_iam_policy",
		F:    testSweepIamPolicies,
		Dependencies: []string{
			"aws_iam_group",
			"aws_iam_role",
			"aws_iam_user",
		},
	})
}

func testSweepIamPolicies(region string) error {
	client, err := acctest.SharedRegionalSweeperClient(region)

	if err != nil {
		return fmt.Errorf("error getting client: %w", err)
	}

	conn := client.(*conns.AWSClient).IAMConn
	input := &iam.ListPoliciesInput{
		Scope: aws.String(iam.PolicyScopeTypeLocal),
	}
	var sweeperErrs *multierror.Error

	err = conn.ListPoliciesPages(input, func(page *iam.ListPoliciesOutput, lastPage bool) bool {
		if page == nil {
			return !lastPage
		}

		for _, policy := range page.Policies {
			arn := aws.StringValue(policy.Arn)
			input := &iam.DeletePolicyInput{
				PolicyArn: policy.Arn,
			}

			log.Printf("[INFO] Deleting IAM Policy: %s", arn)
			if err := iamPolicyDeleteNondefaultVersions(arn, conn); err != nil {
				sweeperErr := fmt.Errorf("error deleting IAM Policy (%s) non-default versions: %w", arn, err)
				log.Printf("[ERROR] %s", sweeperErr)
				sweeperErrs = multierror.Append(sweeperErrs, sweeperErr)
				continue
			}

			_, err := conn.DeletePolicy(input)

			// Treat this sweeper as best effort for now. There are a lot of edge cases
			// with lingering aws_iam_role resources in the HashiCorp testing accounts.
			if tfawserr.ErrMessageContains(err, iam.ErrCodeDeleteConflictException, "") {
				log.Printf("[WARN] Ignoring IAM Policy (%s) deletion error: %s", arn, err)
				continue
			}

			if tfawserr.ErrMessageContains(err, iam.ErrCodeNoSuchEntityException, "") {
				continue
			}

			if err != nil {
				sweeperErr := fmt.Errorf("error deleting IAM Policy (%s): %w", arn, err)
				log.Printf("[ERROR] %s", sweeperErr)
				sweeperErrs = multierror.Append(sweeperErrs, sweeperErr)
				continue
			}
		}

		return !lastPage
	})

	if acctest.SkipSweepError(err) {
		log.Printf("[WARN] Skipping IAM Policy sweep for %s: %s", region, err)
		return sweeperErrs.ErrorOrNil()
	}

	if err != nil {
		sweeperErrs = multierror.Append(sweeperErrs, fmt.Errorf("error retrieving IAM Policies: %w", err))
	}

	return sweeperErrs.ErrorOrNil()
}

func TestAccAWSIAMPolicy_basic(t *testing.T) {
	var out iam.GetPolicyOutput
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")
	resourceName := "aws_iam_policy.test"
	expectedPolicyText := `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": [
        "ec2:Describe*"
      ],
      "Effect": "Allow",
      "Resource": "*"
    }
  ]
}
`
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, iam.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSIAMPolicyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSIAMPolicyConfigName(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSIAMPolicyExists(resourceName, &out),
					acctest.CheckResourceAttrGlobalARN(resourceName, "arn", "iam", fmt.Sprintf("policy/%s", rName)),
					resource.TestCheckResourceAttr(resourceName, "description", ""),
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttr(resourceName, "path", "/"),
					resource.TestCheckResourceAttr(resourceName, "policy", expectedPolicyText),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "0"),
					resource.TestCheckResourceAttrSet(resourceName, "policy_id"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSIAMPolicy_description(t *testing.T) {
	var out iam.GetPolicyOutput
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")
	resourceName := "aws_iam_policy.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, iam.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSIAMPolicyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSIAMPolicyConfigDescription(rName, "description1"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSIAMPolicyExists(resourceName, &out),
					resource.TestCheckResourceAttr(resourceName, "description", "description1"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSIAMPolicy_tags(t *testing.T) {
	var out iam.GetPolicyOutput
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")
	resourceName := "aws_iam_policy.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, iam.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSIAMPolicyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSIAMPolicyConfigTags1(rName, "key1", "value1"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSIAMPolicyExists(resourceName, &out),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "1"),
					resource.TestCheckResourceAttr(resourceName, "tags.key1", "value1"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccAWSIAMPolicyConfigTags2(rName, "key1", "value1updated", "key2", "value2"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSIAMPolicyExists(resourceName, &out),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "2"),
					resource.TestCheckResourceAttr(resourceName, "tags.key1", "value1updated"),
					resource.TestCheckResourceAttr(resourceName, "tags.key2", "value2"),
				),
			},
			{
				Config: testAccAWSIAMPolicyConfigTags1(rName, "key2", "value2"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSIAMPolicyExists(resourceName, &out),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "1"),
					resource.TestCheckResourceAttr(resourceName, "tags.key2", "value2"),
				),
			},
		},
	})
}

func TestAccAWSIAMPolicy_disappears(t *testing.T) {
	var out iam.GetPolicyOutput
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")
	resourceName := "aws_iam_policy.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, iam.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSIAMPolicyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSIAMPolicyConfigName(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSIAMPolicyExists(resourceName, &out),
					acctest.CheckResourceDisappears(acctest.Provider, ResourcePolicy(), resourceName),
				),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccAWSIAMPolicy_namePrefix(t *testing.T) {
	var out iam.GetPolicyOutput
	namePrefix := "tf-acc-test-"
	resourceName := "aws_iam_policy.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, iam.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSIAMPolicyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSIAMPolicyConfigNamePrefix(namePrefix),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSIAMPolicyExists(resourceName, &out),
					resource.TestMatchResourceAttr(resourceName, "name", regexp.MustCompile(fmt.Sprintf("^%s", namePrefix))),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"name_prefix"},
			},
		},
	})
}

func TestAccAWSIAMPolicy_path(t *testing.T) {
	var out iam.GetPolicyOutput
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")
	resourceName := "aws_iam_policy.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, iam.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSIAMPolicyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSIAMPolicyConfigPath(rName, "/path1/"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSIAMPolicyExists(resourceName, &out),
					resource.TestCheckResourceAttr(resourceName, "path", "/path1/"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSIAMPolicy_policy(t *testing.T) {
	var out iam.GetPolicyOutput
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")
	resourceName := "aws_iam_policy.test"
	policy1 := "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Action\":[\"ec2:Describe*\"],\"Effect\":\"Allow\",\"Resource\":\"*\"}]}"
	policy2 := "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Action\":[\"ec2:*\"],\"Effect\":\"Allow\",\"Resource\":\"*\"}]}"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, iam.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSIAMPolicyDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccAWSIAMPolicyConfigPolicy(rName, "not-json"),
				ExpectError: regexp.MustCompile("invalid JSON"),
			},
			{
				Config: testAccAWSIAMPolicyConfigPolicy(rName, policy1),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSIAMPolicyExists(resourceName, &out),
					resource.TestCheckResourceAttr(resourceName, "policy", policy1),
				),
			},
			{
				Config: testAccAWSIAMPolicyConfigPolicy(rName, policy2),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSIAMPolicyExists(resourceName, &out),
					resource.TestCheckResourceAttr(resourceName, "policy", policy2),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccCheckAWSIAMPolicyExists(resource string, res *iam.GetPolicyOutput) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resource]
		if !ok {
			return fmt.Errorf("Not found: %s", resource)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No Policy name is set")
		}

		conn := acctest.Provider.Meta().(*conns.AWSClient).IAMConn

		resp, err := conn.GetPolicy(&iam.GetPolicyInput{
			PolicyArn: aws.String(rs.Primary.Attributes["arn"]),
		})
		if err != nil {
			return err
		}

		*res = *resp

		return nil
	}
}

func testAccCheckAWSIAMPolicyDestroy(s *terraform.State) error {
	conn := acctest.Provider.Meta().(*conns.AWSClient).IAMConn

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "aws_iam_policy" {
			continue
		}

		_, err := conn.GetPolicy(&iam.GetPolicyInput{
			PolicyArn: aws.String(rs.Primary.ID),
		})

		if tfawserr.ErrMessageContains(err, iam.ErrCodeNoSuchEntityException, "") {
			continue
		}

		if err != nil {
			return err
		}

		return fmt.Errorf("IAM Policy (%s) still exists", rs.Primary.ID)
	}

	return nil
}

func testAccAWSIAMPolicyConfigDescription(rName, description string) string {
	return fmt.Sprintf(`
resource "aws_iam_policy" "test" {
  description = %q
  name        = %q

  policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": [
        "ec2:Describe*"
      ],
      "Effect": "Allow",
      "Resource": "*"
    }
  ]
}
EOF
}
`, description, rName)
}

func testAccAWSIAMPolicyConfigName(rName string) string {
	return fmt.Sprintf(`
resource "aws_iam_policy" "test" {
  name = %q

  policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": [
        "ec2:Describe*"
      ],
      "Effect": "Allow",
      "Resource": "*"
    }
  ]
}
EOF
}
`, rName)
}

func testAccAWSIAMPolicyConfigNamePrefix(namePrefix string) string {
	return fmt.Sprintf(`
resource "aws_iam_policy" "test" {
  name_prefix = %q

  policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": [
        "ec2:Describe*"
      ],
      "Effect": "Allow",
      "Resource": "*"
    }
  ]
}
EOF
}
`, namePrefix)
}

func testAccAWSIAMPolicyConfigPath(rName, path string) string {
	return fmt.Sprintf(`
resource "aws_iam_policy" "test" {
  name = %q
  path = %q

  policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": [
        "ec2:Describe*"
      ],
      "Effect": "Allow",
      "Resource": "*"
    }
  ]
}
EOF
}
`, rName, path)
}

func testAccAWSIAMPolicyConfigPolicy(rName, policy string) string {
	return fmt.Sprintf(`
resource "aws_iam_policy" "test" {
  name   = %q
  policy = %q
}
`, rName, policy)
}

func testAccAWSIAMPolicyConfigTags1(rName, tagKey1, tagValue1 string) string {
	return fmt.Sprintf(`
resource "aws_iam_policy" "test" {
  name = %q

  policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": [
        "ec2:Describe*"
      ],
      "Effect": "Allow",
      "Resource": "*"
    }
  ]
}
EOF

  tags = {
    %[2]q = %[3]q
  }
}
`, rName, tagKey1, tagValue1)
}

func testAccAWSIAMPolicyConfigTags2(rName, tagKey1, tagValue1, tagKey2, tagValue2 string) string {
	return fmt.Sprintf(`
resource "aws_iam_policy" "test" {
  name = %q

  policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": [
        "ec2:Describe*"
      ],
      "Effect": "Allow",
      "Resource": "*"
    }
  ]
}
EOF

  tags = {
    %[2]q = %[3]q
    %[4]q = %[5]q
  }
}
`, rName, tagKey1, tagValue1, tagKey2, tagValue2)
}