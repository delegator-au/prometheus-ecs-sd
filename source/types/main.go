package types

type StaticConfig struct {
	Targets []string
	Labels  Labels
}

type Labels struct {
	ContainerName        string `yaml:"ContainerName"`
	ContainerId          string `yaml:"ContainerId"`
	ContainerImage       string `yaml:"ContainerImage"`
	TaskDefinitionFamily string `yaml:"TaskDefinitionFamily"`
	TaskRevision         int32  `yaml:"TaskRevision"`
	InstanceType         string `yaml:"InstanceType"`
	SubnetId             string `yaml:"SubnetId"`
	VpcId                string `yaml:"VpcId"`
	ClusterArn           string `yaml:"ClusterArn"`
	MetricsPath          string `yaml:"__metrics_path__,omitempty"`
	Scheme               string `yaml:"__scheme__,omitempty"`
}

type PrometheusDockerLabels struct {
	Port   *string
	Path   string
	Scheme string
}
