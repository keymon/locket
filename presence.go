package locket

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/consuladapter"
	"github.com/hashicorp/consul/api"
	"github.com/nu7hatch/gouuid"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
)

type Presence struct {
	consul *Session
	key    string
	value  []byte

	clock         clock.Clock
	retryInterval time.Duration

	logger lager.Logger
}

func NewPresence(
	logger lager.Logger,
	consulClient consuladapter.Client,
	lockKey string,
	lockValue []byte,
	clock clock.Clock,
	retryInterval time.Duration,
	lockTTL time.Duration,
) Presence {
	uuid, err := uuid.NewV4()
	if err != nil {
		logger.Fatal("create-uuid-failed", err)
	}

	session, err := NewSessionNoChecks(uuid.String(), lockTTL, consulClient)
	if err != nil {
		logger.Fatal("consul-session-failed", err)
	}

	return Presence{
		consul: session,
		key:    lockKey,
		value:  lockValue,

		clock:         clock,
		retryInterval: retryInterval,

		logger: logger,
	}
}

func (p Presence) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	logger := p.logger.Session("presence", lager.Data{"key": p.key, "value": string(p.value)})
	logger.Info("starting")

	defer func() {
		logger.Info("cleaning-up")
		p.consul.Destroy()
		logger.Info("done")
	}()

	type presenceResult struct {
		presenceLost <-chan string
		err          error
	}

	presenceCh := make(chan presenceResult, 1)
	setPresence := func(session *Session) {
		logger.Info("setting-presence")
		presenceLost, err := session.SetPresence(p.key, p.value)
		presenceCh <- presenceResult{presenceLost, err}
	}

	var retryTimer <-chan time.Time
	var presenceLost <-chan string
	var lastConsulErr error

	go setPresence(p.consul)

	logger.Info("started")

	readyChanClosed := false

	for {
		select {
		case sig := <-signals:
			logger.Info("shutting-down", lager.Data{"received-signal": sig})

			return nil
		case lastConsulErr = <-p.consul.Err():
			var data lager.Data
			if lastConsulErr != nil {
				data = lager.Data{"err": lastConsulErr.Error()}
			}
			logger.Info("consul-error", data)

			retryTimer = p.clock.NewTimer(p.retryInterval).C()
		case result := <-presenceCh:
			if result.err == nil {
				if !readyChanClosed {
					close(ready)
					readyChanClosed = true
				}
				logger.Info("succeeded-setting-presence")

				retryTimer = nil
				presenceLost = result.presenceLost
			} else {
				logger.Error("failed-setting-presence", result.err)

				retryTimer = p.clock.NewTimer(p.retryInterval).C()
			}
		case <-presenceLost:
			logger.Info("presence-lost")

			presenceLost = nil
			retryTimer = p.clock.NewTimer(p.retryInterval).C()
		case <-retryTimer:
			if lastConsulErr == api.ErrSessionExpired || lastConsulErr == ErrInvalidSession {
				logger.Info("recreating-session")
				newSession, err := p.consul.Recreate()
				if err != nil {
					logger.Error("failed-recreating-session", err)
					retryTimer = p.clock.NewTimer(p.retryInterval).C()
					continue
				} else {
					logger.Info("succeeded-recreating-session")
					p.consul = newSession
					retryTimer = nil
				}
			}
			go setPresence(p.consul)
		}
	}
}
