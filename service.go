package locket

import (
	"github.com/hashicorp/consul/api"
	"github.com/pivotal-golang/lager"
	"os"
	"time"
)

type Service struct {
	client      *api.Client
	serviceName string
	serviceTTL  time.Duration
	logger      lager.Logger
}

func NewService(consulClient *api.Client, serviceName string, serviceTTL time.Duration, logger lager.Logger) Service {
	return Service{
		client:      consulClient,
		serviceName: serviceName,
		serviceTTL:  serviceTTL,
		logger:      logger,
	}
}

func (s Service) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	logger := s.logger.Session("service", lager.Data{"service-name": s.serviceName, "service-TTL": s.serviceTTL.String()})
	logger.Info("starting")
	agent := s.client.Agent()
	var err error

	for i := 0; i < 5; i++ {
		err = s.register(logger)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		return err
	}

	checkID := "service:" + s.serviceName

	err = agent.PassTTL(checkID, "")
	if err != nil {
		logger.Error("failed-passing-ttl", err)
	}
	close(ready)
	logger.Info("started")

	defer func() {
		logger.Debug("failing-ttl")
		err := agent.FailTTL(checkID, "")
		if err != nil {
			logger.Error("failed-failing-ttl", err)
		}
	}()

	timer := time.NewTimer(s.serviceTTL / 2)

	for {
		select {
		case sig := <-signals:
			logger.Info("shutting-down", lager.Data{"received-signal": sig})
			return nil
		case <-timer.C:
			logger.Debug("passing-ttl")
			err = agent.PassTTL(checkID, "")
			if err != nil {
				logger.Error("failed-passing-ttl", err)
			}
		}
	}
	return nil
}

func (s *Service) register(logger lager.Logger) error {
	agent := s.client.Agent()
	logger.Info("registering-service")
	err := agent.ServiceRegister(&api.AgentServiceRegistration{
		ID:   s.serviceName,
		Name: s.serviceName,
		Check: &api.AgentServiceCheck{
			TTL: s.serviceTTL.String(),
		},
	})
	if err != nil {
		logger.Error("failed-registering-service", err)
		return err
	} else {
		logger.Info("succeeded-registering-service")
		return nil
	}
}
