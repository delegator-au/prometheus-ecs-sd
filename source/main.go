package main

import (
	"context"
	"delegator.com.au/prometheus-ecs-sd/clients"
	"delegator.com.au/prometheus-ecs-sd/logger"
	"delegator.com.au/prometheus-ecs-sd/types"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecsTypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"strconv"
	"time"
)

const (
	PrometheusScrapePort       = "PROMETHEUS_SCRAPE_PORT"
	PrometheusMetricsPath      = "PROMETHEUS_METRICS_PATH"
	PrometheusMetricsScheme    = "PROMETHEUS_METRICS_SCHEME"
	PrometheusStaticConfigPath = "ecs_file_sd.yml"
	ECSCluster                 = "ECS_CLUSTER"
	ScrapeInterval             = "SCRAPE_INTERVAL"
	DefaultScrapeInterval      = 120
)

var ctx = context.TODO()

// getContainerInstances returns each ContainerInstance in the cluster
func getContainerInstances(cluster *string) ([]ecsTypes.ContainerInstance, error) {
	listInput := &ecs.ListContainerInstancesInput{Cluster: cluster}
	listResponse, err := clients.ECSClient.ListContainerInstances(ctx, listInput)
	if err != nil {
		logger.Log.Error().Err(err).Msg("Failed to ListContainerInstances")
		return []ecsTypes.ContainerInstance{}, err
	}

	if len(listResponse.ContainerInstanceArns) == 0 {
		logger.Log.Info().Msg("There were no container instances")
		return []ecsTypes.ContainerInstance{}, nil
	}

	descInput := &ecs.DescribeContainerInstancesInput{
		Cluster:            cluster,
		ContainerInstances: listResponse.ContainerInstanceArns,
	}
	descResponse, err := clients.ECSClient.DescribeContainerInstances(ctx, descInput)
	if err != nil {
		logger.Log.Error().Err(err).Msg("Failed to DescribeContainerInstances")
		return []ecsTypes.ContainerInstance{}, err
	}

	logger.Log.Info().Msg(fmt.Sprintf("Found %d container instances", len(descResponse.ContainerInstances)))
	return descResponse.ContainerInstances, nil
}

// getTasks returns each Task that is running on an instance within the cluster
func getTasks(cluster *string, instance *string) ([]ecsTypes.Task, error) {
	listInput := &ecs.ListTasksInput{
		Cluster:           cluster,
		ContainerInstance: instance,
	}
	listResponse, err := clients.ECSClient.ListTasks(ctx, listInput)
	if err != nil {
		logger.Log.Error().Err(err).Msg("Failed to ListTasks")
		return []ecsTypes.Task{}, err
	}

	if len(listResponse.TaskArns) == 0 {
		logger.Log.Info().Str("instance", *instance).Msg("There were no tasks on the instance")
		return []ecsTypes.Task{}, nil
	}

	descInput := &ecs.DescribeTasksInput{
		Tasks:   listResponse.TaskArns,
		Cluster: cluster,
	}
	descResponse, err := clients.ECSClient.DescribeTasks(ctx, descInput)
	if err != nil {
		logger.Log.Error().Err(err).Msg("Failed to DescribeTasks")
		return []ecsTypes.Task{}, err
	}

	logger.Log.Info().Msg(fmt.Sprintf("Found %d tasks", len(descResponse.Tasks)))
	return descResponse.Tasks, nil
}

// getTaskDefinition returns an ECS TaskDefinition
func getTaskDefinition(arn *string) (*ecsTypes.TaskDefinition, error) {
	input := &ecs.DescribeTaskDefinitionInput{TaskDefinition: arn}
	response, err := clients.ECSClient.DescribeTaskDefinition(ctx, input)

	if err != nil {
		logger.Log.Error().Err(err).Msg("Failed to DescribeTaskDefinition")
		return nil, err
	}

	return response.TaskDefinition, nil
}

// getEC2Instance returns an EC2 Instance
func getEC2Instance(instanceId string) *ec2Types.Instance {
	input := &ec2.DescribeInstancesInput{InstanceIds: []string{instanceId}}
	response, err := clients.EC2Client.DescribeInstances(ctx, input)

	if err != nil {
		logger.Log.Error().Err(err).Msg("Failed to DescribeTaskDefinition")
		return nil
	}

	if len(response.Reservations[0].Instances) == 0 {
		logger.Log.Error().Str("instanceId", instanceId).Msg("Cannot find instance")
		return nil
	}

	if len(response.Reservations[0].Instances) != 1 {
		logger.Log.Warn().Msg(fmt.Sprintf("Expected 1 instance but found %d", len(response.Reservations[0].Instances)))
	}

	return &response.Reservations[0].Instances[0]
}

// getContainerPrometheusLabels searches a task definition for the container name and returns prometheus specific Docker labels
func getContainerPrometheusLabels(name *string, definition ecsTypes.TaskDefinition) *types.PrometheusDockerLabels {
	// defaults
	labels := &types.PrometheusDockerLabels{
		Path:   "/metrics",
		Scheme: "http",
	}

	// search the container definitions for the requested container name to get its labels
	for _, container := range definition.ContainerDefinitions {
		if *container.Name == *name {
			if val, ok := container.DockerLabels[PrometheusScrapePort]; ok {
				labels.Port = &val
			} else {
				logger.Log.Info().Str("name", *name).Msg("Container has no PrometheusScrapePort Docker label")
				return nil
			}
			if val, ok := container.DockerLabels[PrometheusMetricsPath]; ok {
				labels.Path = val
			}
			if val, ok := container.DockerLabels[PrometheusMetricsScheme]; ok {
				labels.Scheme = val
			}
			return labels
		}
	}

	// didn't find the container name in the definition
	logger.Log.Warn().Str("name", *name).Str("definition", *definition.TaskDefinitionArn).Msg("Unable to find the container in the provided task definition")
	return nil
}

// createStaticConfig creates a Prometheus StaticConfig for service discovery from ECS components
func createStaticConfig(
	container ecsTypes.Container,
	taskDefinition ecsTypes.TaskDefinition,
	instance ecsTypes.ContainerInstance,
	cluster *string,
) *types.StaticConfig {

	// target is an IPAddress:Port combination
	var target string
	var prometheusLabels *types.PrometheusDockerLabels

	// find the prometheus labels on the container
	if prometheusLabels = getContainerPrometheusLabels(container.Name, taskDefinition); prometheusLabels == nil {
		logger.Log.Info().Str("name", *container.Name).Msg("Skipping container")
		return nil
	}

	ec2Instance := getEC2Instance(*instance.Ec2InstanceId)
	if ec2Instance == nil {
		return nil
	}

	// get scrape IP address
	if taskDefinition.NetworkMode == ecsTypes.NetworkModeAwsvpc {
		target = fmt.Sprintf("%s:%s", *container.NetworkInterfaces[0].PrivateIpv4Address, *prometheusLabels.Port)
	} else {
		target = fmt.Sprintf("%s:%s", *ec2Instance.PrivateIpAddress, *prometheusLabels.Port)
	}

	// get other labels
	labels := types.Labels{
		ContainerName:        *container.Name,
		ContainerId:          *container.RuntimeId,
		ContainerImage:       *container.Image,
		TaskDefinitionFamily: *taskDefinition.Family,
		TaskRevision:         taskDefinition.Revision,
		InstanceType:         string(ec2Instance.InstanceType),
		SubnetId:             *ec2Instance.SubnetId,
		VpcId:                *ec2Instance.VpcId,
		ClusterArn:           *cluster,
		MetricsPath:          prometheusLabels.Path,
		Scheme:               prometheusLabels.Scheme,
	}

	return &types.StaticConfig{
		Targets: []string{target},
		Labels:  labels,
	}
}

// writeConfig writes the StaticConfig out to a yml file
func writeConfig(staticConfigs []*types.StaticConfig) {
	marshalled, err := yaml.Marshal(staticConfigs)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Unable to marshal StaticConfig to yaml")
	}

	err = ioutil.WriteFile(PrometheusStaticConfigPath, marshalled, 0644)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Unable to write StaticConfig out to file")
	}
}

func parseTargets(cluster *string) {
	var staticConfigs []*types.StaticConfig

	instances, instanceErr := getContainerInstances(cluster)
	if instanceErr != nil {
		logger.Log.Fatal().Msg("Unable to get the container instances for the cluster")
	}

	for _, instance := range instances {

		// get all of the
		runningTasks, tasksErr := getTasks(cluster, instance.ContainerInstanceArn)
		if tasksErr != nil {
			continue
		}

		for _, task := range runningTasks {
			for _, container := range task.Containers {

				// get the task definition and skip if there's an issue
				taskDefinition, taskDefErr := getTaskDefinition(task.TaskDefinitionArn)
				if taskDefErr != nil {
					continue
				}

				staticConfig := createStaticConfig(container, *taskDefinition, instance, cluster)

				// staticConfig is nil if there's an issue or if the container is missing Docker labels
				if staticConfig != nil {
					staticConfigs = append(staticConfigs, staticConfig)
				}
			}
		}
	}

	// write to file
	writeConfig(staticConfigs)

	logger.Log.Info().Msg("Done!")
}

func main() {
	// get the cluster from the environment
	var cluster string
	if v, ok := os.LookupEnv(ECSCluster); ok {
		cluster = v
	} else {
		logger.Log.Fatal().Msg(fmt.Sprintf("Required enviroment variable %s is missing", ECSCluster))
	}

	// get the interval from the environment
	var interval = DefaultScrapeInterval
	var err error
	if v, ok := os.LookupEnv(ScrapeInterval); ok {
		interval, err = strconv.Atoi(v)
		if err != nil {
			logger.Log.Fatal().Err(err).Str(ScrapeInterval, v).Msg(fmt.Sprintf("Unable to convert %s to int32", ScrapeInterval))
		}
	}

	// loop
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	for _ = range ticker.C {
		parseTargets(&cluster)
	}
}
