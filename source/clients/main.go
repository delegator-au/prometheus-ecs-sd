package clients

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
)

var (
	cfg, _    = config.LoadDefaultConfig(context.TODO())
	ECSClient = ecs.NewFromConfig(cfg)
	EC2Client = ec2.NewFromConfig(cfg)
)
