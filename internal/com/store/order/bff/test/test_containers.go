package test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/go-resty/resty/v2"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/tidwall/gjson"
)

func StartSpecmaticMock(t *testing.T, env *TestEnvironment) (testcontainers.Container, error) {
	pwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("error getting current directory: %v", err)
	}

	localReportDirectory := filepath.Join(pwd, "build", "reports")
	if err := os.MkdirAll(localReportDirectory, 0755); err != nil {
		return nil, fmt.Errorf("error creating reports directory: %v", err)
	}

	networkName := env.BffTestNetwork.Name

	req := testcontainers.ContainerRequest{
		Image: "specmatic/enterprise",
		ExposedPorts: []string{
			env.Config.BackendPort + "/tcp",
			env.Config.KafkaPort + "/tcp",
			env.Config.KafkaAPIPort + "/tcp",
		},
		Networks: []string{networkName},
		NetworkAliases: map[string][]string{
			networkName: {env.Config.BackendHost, env.Config.KafkaHost},
		},
		Cmd: []string{"mock"},
		Mounts: testcontainers.Mounts(
			testcontainers.BindMount(filepath.Join(pwd, "specmatic.yaml"), "/usr/src/app/specmatic.yaml"),
			testcontainers.BindMount(localReportDirectory, "/usr/src/app/build/reports/specmatic"),
		),
		Env: map[string]string{
			"KAFKA_EXTERNAL_HOST": env.Config.KafkaHost,
			"KAFKA_EXTERNAL_PORT": env.Config.KafkaPort,
			"API_SERVER_PORT":     env.Config.KafkaAPIPort,
		},
		WaitingFor: wait.ForLog("AsyncMock has started").WithStartupTimeout(2 * time.Minute),
	}

	mockC, err := testcontainers.GenericContainer(env.Ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("error starting specmatic mock container: %v", err)
	}

	mappedApiPort, err := mockC.MappedPort(env.Ctx, nat.Port(env.Config.KafkaAPIPort))
	if err != nil {
		return nil, fmt.Errorf("error getting Kafka API port: %v", err)
	}
	env.KafkaDynamicAPIPort = mappedApiPort.Port()

	kafkaAPIHost, err := mockC.Host(env.Ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting host IP: %v", err)
	}
	env.KafkaAPIHost = kafkaAPIHost

	if err := SetKafkaExpectations(env); err != nil {
		t.Logf("failed to set Kafka expectations: %v", err)
	}

	logReader, err := mockC.Logs(env.Ctx)
	if err == nil {
		var logBuf bytes.Buffer
		io.Copy(&logBuf, logReader)
		logReader.Close()
		t.Logf("=== Specmatic mock startup logs ===\n%s", logBuf.String())

		re := regexp.MustCompile(`EXTERNAL://0\.0\.0\.0:(\d+)`)
		if matches := re.FindStringSubmatch(logBuf.String()); len(matches) > 1 {
			env.KafkaExternalPort = matches[1]
			t.Logf("Kafka EXTERNAL listener port: %s", env.KafkaExternalPort)
		}
	}

	t.Log("Specmatic mock container started (HTTP stub + Kafka mock)")
	return mockC, nil
}

func StartBFFService(t *testing.T, env *TestEnvironment) (testcontainers.Container, string, error) {
	networkName := env.BffTestNetwork.Name
	dockerfilePath := "Dockerfile"
	contextPath := "."

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:       contextPath,
			Dockerfile:    dockerfilePath,
			PrintBuildLog: true,
		},
		Env: map[string]string{
			"DOMAIN_SERVER_PORT": env.Config.BackendPort,
			"DOMAIN_SERVER_HOST": env.Config.BackendHost,
			"KAFKA_PORT":         env.KafkaExternalPort,
			"KAFKA_HOST":         env.Config.KafkaHost,
		},
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

	return bffContainer, env.Config.BFFServerPort, nil
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
		Image: "specmatic/enterprise",
		Env: map[string]string{
			"SPECMATIC_GENERATIVE_TESTS": "true",
			"FILTER":                     "'/health'",
			"APP_URL":                    fmt.Sprintf("http://bff-service:%d", bffPortInt),
		},
		Cmd: []string{"test"},
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

	// Stop with a graceful timeout so specmatic can finish writing CTRF reports before the container exits
	defer func() {
		stopTimeout := 5 * time.Minute
		testContainer.Stop(env.Ctx, &stopTimeout)
	}()

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
