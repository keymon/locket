package locket_test

import (
	"time"

	"github.com/cloudfoundry-incubator/locket"
	"github.com/hashicorp/consul/api"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
)

var _ = FDescribe("Service", func() {
	var (
		serviceName string
		ttl         time.Duration

		serviceRunner  ifrit.Runner
		serviceProcess ifrit.Process

		consulClient *api.Client
		serviceClock clock.Clock

		logger lager.Logger
	)

	BeforeEach(func() {
		serviceClock = clock.NewClock()
		consulClient = consulRunner.NewClient()
		serviceName = "my-service"
		ttl = 50 * time.Millisecond
		logger = lagertest.NewTestLogger("locket")
	})

	JustBeforeEach(func() {
		serviceRunner = locket.NewService(logger, serviceName, ttl, serviceClock, consulClient)
	})

	AfterEach(func() {
		ginkgomon.Kill(serviceProcess)
	})

	Context("when consul is running", func() {
		It("registers the service", func() {
			serviceProcess = ifrit.Background(serviceRunner)
			Eventually(serviceProcess.Ready()).Should(BeClosed())

			agent := consulClient.Agent()

			Expect(agent.Services()).To(HaveKey("my-service"))
		})

		It("marks the service as passing", func() {
			serviceProcess = ifrit.Background(serviceRunner)
			Eventually(serviceProcess.Ready()).Should(BeClosed())
			agent := consulClient.Agent()
			checks, err := agent.Checks()
			Expect(err).NotTo(HaveOccurred())
			Expect(checks).To(HaveKey("service:my-service"))
			Expect(checks["service:my-service"].Status).To(Equal("passing"))
		})

		It("continues to pass after the ttl expires", func() {
			serviceProcess = ifrit.Background(serviceRunner)
			Eventually(serviceProcess.Ready()).Should(BeClosed())
			agent := consulClient.Agent()

			Consistently(func() string {
				checks, err := agent.Checks()
				Expect(err).NotTo(HaveOccurred())
				Expect(checks).To(HaveKey("service:my-service"))
				return checks["service:my-service"].Status
			}).Should(Equal("passing"))
		})

		It("fails to pass after service is stopped", func() {
			serviceProcess = ifrit.Background(serviceRunner)
			Eventually(serviceProcess.Ready()).Should(BeClosed())
			agent := consulClient.Agent()

			ginkgomon.Interrupt(serviceProcess)
			checks, err := agent.Checks()
			Expect(err).NotTo(HaveOccurred())
			Expect(checks).To(HaveKey("service:my-service"))
			Expect(checks["service:my-service"].Status).To(Equal("critical"))
		})

		Context("when the service stops heartbeating", func() {
			var fakeClock *fakeclock.FakeClock
			BeforeEach(func() {
				fakeClock = fakeclock.NewFakeClock(time.Now())
				serviceClock = fakeClock
			})

			It("expires the ttl", func() {
				serviceProcess = ifrit.Background(serviceRunner)
				Eventually(serviceProcess.Ready()).Should(BeClosed())
				agent := consulClient.Agent()

				checks, err := agent.Checks()
				Expect(err).NotTo(HaveOccurred())
				Expect(checks).To(HaveKey("service:my-service"))
				Expect(checks["service:my-service"].Status).To(Equal("passing"))

				Eventually(func() string {
					checks, err := agent.Checks()
					Expect(err).NotTo(HaveOccurred())
					Expect(checks).To(HaveKey("service:my-service"))
					return checks["service:my-service"].Status
				}).Should(Equal("critical"))

				fakeClock.Increment(5 * ttl)
				Eventually(func() string {
					checks, err := agent.Checks()
					Expect(err).NotTo(HaveOccurred())
					Expect(checks).To(HaveKey("service:my-service"))
					return checks["service:my-service"].Status
				}).Should(Equal("passing"))
			})
		})
	})

	Context("When consul is down", func() {
		BeforeEach(func() {
			consulRunner.Stop()
		})

		AfterEach(func() {
			consulRunner.Start()
			consulRunner.WaitUntilReady()
		})

		It("continues to retry registering the service", func() {
			serviceProcess = ifrit.Background(serviceRunner)

			Eventually(serviceProcess.Ready()).Should(BeClosed())
			Consistently(serviceProcess.Wait()).ShouldNot(Receive())

			Eventually(logger).Should(Say("failed-registering-service"))
			Eventually(logger).Should(Say("registering-service"))
			Eventually(logger).Should(Say("failed-registering-service"))
			Eventually(logger).Should(Say("registering-service"))
		})

		It("still exits when signaled", func() {
			serviceProcess = ifrit.Background(serviceRunner)

			Eventually(serviceProcess.Ready()).Should(BeClosed())
			Consistently(serviceProcess.Wait()).ShouldNot(Receive())

			ginkgomon.Interrupt(serviceProcess)

			Eventually(serviceProcess.Wait()).ShouldNot(Receive())
		})

		Context("when consul starts up", func() {
			It("registers the service", func() {
				serviceProcess = ifrit.Background(serviceRunner)
				Eventually(serviceProcess.Ready()).Should(BeClosed())

				Eventually(logger).Should(Say("failed-registering-service"))
				Eventually(logger).Should(Say("registering-service"))
				Consistently(serviceProcess.Wait()).ShouldNot(Receive())

				consulRunner.Start()
				consulRunner.WaitUntilReady()

				agent := consulClient.Agent()

				Expect(agent.Services()).To(HaveKey("my-service"))
			})
		})
	})
})
