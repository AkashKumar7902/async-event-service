package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	defaultAddr          = ":8080"
	defaultWorkerCount   = 4
	defaultQueueCapacity = 1024
)

// Config contains the few settings that are useful to tune between deployments.
type Config struct {
	Addr          string
	WorkerCount   int
	QueueCapacity int
}

// Load reads configuration from the environment and otherwise uses defaults.
func Load() (Config, error) {
	workerCount, workerErr := positiveInt("WORKER_COUNT", defaultWorkerCount)
	queueCapacity, queueErr := positiveInt("QUEUE_CAPACITY", defaultQueueCapacity)
	if err := errors.Join(workerErr, queueErr); err != nil {
		return Config{}, err
	}

	addr := strings.TrimSpace(os.Getenv("HTTP_ADDR"))
	if addr == "" {
		addr = defaultAddr
	}

	return Config{
		Addr:          addr,
		WorkerCount:   workerCount,
		QueueCapacity: queueCapacity,
	}, nil
}

func positiveInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, raw)
	}
	return value, nil
}
