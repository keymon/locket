package locket

import (
	"os"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
)

type Service struct {
	apiClient *api.Client
	serName   string
	logger    lager.Logger
	port      int
	address   string
	clock     clock.Clock
}

func NewService(
	apiClient *api.Client,
	logger lager.Logger,
	serName string,
	port int,
	address string,
	clock clock.Clock,
) Service {
	return Service{
		apiClient: apiClient,
		logger:    logger,
		serName:   serName,
		port:      port,
		address:   address,
		clock:     clock,
	}
}

func (s Service) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	logger := s.logger.Session("service-registartion")
	time.Sleep(time.Millisecond)

	errCh := make(chan error)
	go func() {
		errCh <- s.register(logger)
	}()

	close(ready)

	var timer <-chan time.Time

	for {
		select {
		case sig := <-signals:
			logger.Info("received-signal", lager.Data{"Signal": sig})
			err := s.deRegister(logger)
			if err != nil {
				logger.Error("failed-to-deregister", err)
			}
			return nil
		case err := <-errCh:
			logger.Error("failed-to-register", err)
			timer = s.clock.NewTimer(100 * time.Millisecond).C()
		case <-timer:
			go func() {
				errCh <- s.register(logger)
			}()
		}
	}

	return nil
}

func (s Service) register(logger lager.Logger) error {
	logger.Info("try-register-service")
	serviceRegistration := &api.AgentServiceRegistration{
		Name:    s.serName,
		Port:    s.port,
		Address: s.address,
	}
	return s.apiClient.Agent().ServiceRegister(serviceRegistration)
}

func (s Service) deRegister(logger lager.Logger) error {
	logger.Info("try-deregister-service")
	return s.apiClient.Agent().ServiceDeregister(s.serName)
}
