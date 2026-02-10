package test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/go-resty/resty/v2"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/tidwall/gjson"
)

func StartDomainService(t *testing.T, env *TestEnvironment) (testcontainers.Container, string, error) {
	pwd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("Error getting current directory: %v", err)

	}

	port, err := nat.NewPort("tcp", env.Config.BackendPort)
	if err != nil {
		return nil, "", fmt.Errorf("invalid port number: %w", err)
	}

	req := testcontainers.ContainerRequest{
		Image:        "specmatic/specmatic",
		ExposedPorts: []string{port.Port() + "/tcp"},
		Networks: []string{
			env.BffTestNetwork.Name,
		},
		Cmd: []string{"mock"},
		Mounts: testcontainers.Mounts(
			testcontainers.BindMount(filepath.Join(pwd, "specmatic.yaml"), "/usr/src/app/specmatic.yaml"),
		),
		NetworkAliases: map[string][]string{
			env.BffTestNetwork.Name: {env.Config.BackendHost},
		},
		WaitingFor: wait.ForLog("Mock server is running"),
	}

	t.Log("Domain Container created")

	backendC, err := testcontainers.GenericContainer(env.Ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", err
	}

	mappedPort, err := backendC.MappedPort(env.Ctx, port)
	if err != nil {
		return nil, "", err
	}

	return backendC, mappedPort.Port(), nil
}

func StartKafkaMock(t *testing.T, env *TestEnvironment) (testcontainers.Container, string, error) {
	pwd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("Error getting current directory: %v", err)
	}

	port, err := nat.NewPort("tcp", env.Config.KafkaPort)
	if err != nil {
		return nil, "", fmt.Errorf("invalid port number: %w", err)
	}

	networkName := env.BffTestNetwork.Name

	req := testcontainers.ContainerRequest{
		Image:        "specmatic/specmatic-kafka",
		ExposedPorts: []string{port.Port() + "/tcp", env.Config.KafkaAPIPort + "/tcp"},
		Networks: []string{
			networkName,
		},
		NetworkAliases: map[string][]string{
			networkName: {env.Config.KafkaHost},
		},
		Cmd: []string{"mock"},
		Mounts: testcontainers.Mounts(
			testcontainers.BindMount(filepath.Join(pwd, "specmatic.yaml"), "/usr/src/app/specmatic.yaml"),
		),
		Env: map[string]string{
			"KAFKA_EXTERNAL_HOST": env.Config.KafkaHost,
			"KAFKA_EXTERNAL_PORT": env.Config.KafkaPort,
			"API_SERVER_PORT": env.Config.KafkaAPIPort,
		},
		WaitingFor: wait.ForLog("KafkaMock has started").WithStartupTimeout(2 * time.Minute),
	}

	kafkaC, err := testcontainers.GenericContainer(env.Ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		fmt.Printf("Error starting Kafka mock container: %v", err)
	}

	mappedPort, err := kafkaC.MappedPort(env.Ctx, port)
	if err != nil {
		fmt.Printf("Error getting mapped port for Kafka mock: %v", err)
	}

	mappedApiPort, err := kafkaC.MappedPort(env.Ctx, nat.Port(env.Config.KafkaAPIPort))
	if err != nil {
		fmt.Printf("Error getting API server port: %v", err)
	} else {
		env.KafkaDynamicAPIPort = mappedApiPort.Port()
	}

	// Get the host IP
	kafkaAPIHost, err := kafkaC.Host(env.Ctx)
	if err != nil {
		return nil, "", fmt.Errorf("Error getting host IP: %v", err)
	}
	env.KafkaAPIHost = kafkaAPIHost

	if err := SetKafkaExpectations(env); err != nil {
		fmt.Printf("failed to set Kafka expectations ==== : %v", err)
	}

	return kafkaC, mappedPort.Port(), nil
}

func StartBFFService(t *testing.T, env *TestEnvironment) (testcontainers.Container, string, error) {

	port, err := nat.NewPort("tcp", env.Config.BFFServerPort)
	if err != nil {
		return nil, "", fmt.Errorf("invalid port number: %w", err)
	}

	networkName := env.BffTestNetwork.Name
	dockerfilePath := "Dockerfile"
	contextPath := "."

	req := testcontainers.ContainerRequest{
    Name: "specmatic-order-bff-go",
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    contextPath,
			Dockerfile: dockerfilePath,
			PrintBuildLog: true,
		},
		Env: map[string]string{
			"DOMAIN_SERVER_PORT": env.Config.BackendPort,
			"DOMAIN_SERVER_HOST": env.Config.BackendHost,
			"KAFKA_PORT":         env.Config.KafkaPort,
			"KAFKA_HOST":         env.Config.KafkaHost,
		},
		ExposedPorts: []string{port.Port() + "/tcp"},
		Networks: []string{
			env.BffTestNetwork.Name,
		},
		WaitingFor: wait.ForLog("Listening and serving"),
		NetworkAliases: map[string][]string{
			networkName: {"bff-service"},
		},
	}

	bffContainer, err := testcontainers.GenericContainer(env.Ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", err
	}

	dynamicBffPort, err := bffContainer.MappedPort(env.Ctx, port)
	if err != nil {
		return nil, "", err
	}

	return bffContainer, dynamicBffPort.Port(), nil
}

func RunTestContainer(env *TestEnvironment) (string, error) {
	pwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("Error getting current directory: %v", err)
	}

	bffPortInt, err := strconv.Atoi(env.Config.BFFServerPort)
	if err != nil {
		return "", fmt.Errorf("invalid port number: %w", err)
	}

	localReportDirectory := filepath.Join(pwd, "build", "reports")
	if err := os.MkdirAll(localReportDirectory, 0755); err != nil {
		return "", fmt.Errorf("Error creating reports directory: %v", err)
	}

	req := testcontainers.ContainerRequest{
		Image: "specmatic/specmatic",
		Env: map[string]string{
			"SPECMATIC_GENERATIVE_TESTS": "true",
			"FILTER":"'/health'",
		},
		Cmd: []string{"test", fmt.Sprintf("--port=%d", bffPortInt), "--host=bff-service"},
		Mounts: testcontainers.Mounts(
			testcontainers.BindMount(filepath.Join(pwd, "specmatic.yaml"), "/usr/src/app/specmatic.yaml"),
			testcontainers.BindMount(localReportDirectory, "/usr/src/app/build/reports"),
		),
		Networks: []string{
			env.BffTestNetwork.Name,
		},
		WaitingFor: wait.ForLog("Passed Tests:"),
	}

	testContainer, err := testcontainers.GenericContainer(env.Ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})

	if err != nil {
		return "", err
	}

	// Terminate test container post completion
	defer testContainer.Terminate(env.Ctx)

	// Streaming testing logs to terminal
	logReader, err := testContainer.Logs(env.Ctx)
	if err != nil {
		return "", err
	}
	defer logReader.Close()

	var buf bytes.Buffer
	_, err = io.Copy(&buf, logReader)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func SetKafkaExpectations(env *TestEnvironment) error {
	client := resty.New()

  body := fmt.Sprintf(`{
			"expectations": [
				{ "topic": "product-queries", "count": %d }
			]
		}`, env.ExpectedMessageCount)

	_, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		Post(fmt.Sprintf("http://%s:%s/_expectations", env.KafkaAPIHost, env.KafkaDynamicAPIPort))

	return err
}

func VerifyKafkaExpectations(env *TestEnvironment) error {
	client := resty.New()

	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
    Get(fmt.Sprintf("http://%s:%s/_expectations/verification_status", env.KafkaAPIHost, env.KafkaDynamicAPIPort))

	if err != nil {
		return err
	}

	if !gjson.GetBytes(resp.Body(), "success").Bool() {
		return fmt.Errorf("verification failed: %v", gjson.GetBytes(resp.Body(), "errors").Array())
	}

	fmt.Println("Kafka mock expectations were met successfully.")
	return nil
}
