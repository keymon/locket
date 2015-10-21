package locket

import (
	"os"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
)

type Service struct {
	name         string
	consulClient *api.Client
	ttl          time.Duration
	clock        clock.Clock
	logger       lager.Logger
}

func NewService(logger lager.Logger, name string, ttl time.Duration, clock clock.Clock, consulClient *api.Client) Service {
	return Service{logger: logger, name: name, consulClient: consulClient, ttl: ttl, clock: clock}
}

func (s Service) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	logger := s.logger.Session("service-maintainer", lager.Data{"service-name": s.name, "ttl": s.ttl.String()})
	logger.Info("started")
	close(ready)

	failTicker := s.clock.NewTicker(100 * time.Millisecond)
	err := s.registerService(logger)
	for err != nil {
		select {
		case sig := <-signals:
			return logExit(logger, sig)
		case <-failTicker.C():
			logger.Info("registering-service")
			err = s.registerService(logger)
		}
	}
	failTicker.Stop()

	s.consulClient.Agent().PassTTL("service:"+s.name, "")
	defer func() {
		s.consulClient.Agent().FailTTL("service:"+s.name, "")
	}()

	ticker := s.clock.NewTicker(s.ttl / 2)

	for {
		select {
		case sig := <-signals:
			return logExit(logger, sig)
		case <-ticker.C():
			s.consulClient.Agent().PassTTL("service:"+s.name, "")
		}
	}
	return nil
}

func logExit(logger lager.Logger, signal os.Signal) error {
	logger.Info("exiting", lager.Data{"signal": signal})
	return nil
}

func (s Service) registerService(logger lager.Logger) error {
	err := s.consulClient.Agent().ServiceRegister(&api.AgentServiceRegistration{
		ID:    s.name,
		Name:  s.name,
		Check: &api.AgentServiceCheck{TTL: s.ttl.String()},
	})
	if err != nil {
		logger.Error("failed-registering-service", err)
	}
	return err
}
