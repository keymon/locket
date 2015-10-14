package locket_test

import (
	"time"

	"github.com/cloudfoundry-incubator/locket"
	"github.com/hashicorp/consul/api"

	// "github.com/pivotal-golang/clock"
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
		serviceTTL  time.Duration

		consulClient *api.Client

		serviceRunner  ifrit.Runner
		serviceProcess ifrit.Process

		logger lager.Logger
	)

	BeforeEach(func() {
		serviceName = "test-service"
		serviceTTL = 100 * time.Millisecond

		consulClient = consulRunner.NewClient()

		logger = lagertest.NewTestLogger("locket")
	})

	JustBeforeEach(func() {
		serviceRunner = locket.NewService(consulClient, serviceName, serviceTTL, logger)
	})

	AfterEach(func() {
		ginkgomon.Kill(serviceProcess)
	})

	Context("When consul is running", func() {
		Context("and services registration is successful", func() {
			It("registers the service", func() {
				serviceProcess = ifrit.Background(serviceRunner)
				Eventually(serviceProcess.Ready()).Should(BeClosed())
				services, err := consulClient.Agent().Services()
				Expect(err).NotTo(HaveOccurred())
				Expect(services).To(HaveLen(2))
				Expect(services[serviceName].Service).To(Equal(serviceName))
			})

			It("has service checks", func() {
				serviceProcess = ifrit.Background(serviceRunner)
				Eventually(serviceProcess.Ready()).Should(BeClosed())
				checks, err := consulClient.Agent().Checks()
				Expect(err).NotTo(HaveOccurred())
				Expect(checks).To(HaveLen(1))
				Expect(checks["service:"+serviceName].ServiceID).To(Equal(serviceName))
				Expect(checks["service:"+serviceName].Status).To(Equal("passing"))
			})

			It("keeps updating the TTLs", func() {
				serviceProcess = ifrit.Background(serviceRunner)
				Eventually(serviceProcess.Ready()).Should(BeClosed())

				Consistently(func() string {
					checks, err := consulClient.Agent().Checks()
					Expect(err).NotTo(HaveOccurred())
					return checks["service:"+serviceName].Status
				}).Should(Equal("passing"))
			})

			It("fails itself upon exit", func() {
				serviceProcess = ifrit.Background(serviceRunner)
				Eventually(serviceProcess.Ready()).Should(BeClosed())
				ginkgomon.Kill(serviceProcess)
				checks, err := consulClient.Agent().Checks()
				Expect(err).NotTo(HaveOccurred())
				Expect(checks["service:"+serviceName].Status).To(Equal("critical"))
			})

			Context("and consul goes down", func() {
				It("logs the failure and moves on", func() {
					serviceProcess = ifrit.Background(serviceRunner)
					Eventually(serviceProcess.Ready()).Should(BeClosed())

					consulRunner.Stop()
					defer func() {
						consulRunner.Start()
						consulRunner.WaitUntilReady()
					}()

					Eventually(logger).Should(Say("locket.service.failed-passing-ttl"))
					Consistently(serviceProcess.Wait()).ShouldNot(Receive())
				})
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

		It("retries up to 5 times registering the service", func() {
			serviceProcess = ifrit.Background(serviceRunner)

			for i := 0; i < 5; i++ {
				Eventually(logger).Should(Say("locket.service.registering-service"))
				Eventually(logger).Should(Say("locket.service.failed-registering-service"))
			}

			Consistently(logger).ShouldNot(Say("locket.service.registering-service"))

			Eventually(serviceProcess.Wait()).Should(Receive())
		})

		Context("when consul starts up", func() {
			It("registers the service", func() {
				serviceProcess = ifrit.Background(serviceRunner)

				Eventually(logger).Should(Say("locket.service.registering-service"))
				Eventually(logger).Should(Say("locket.service.failed-registering-service"))

				consulRunner.Start()
				consulRunner.WaitUntilReady()

				Eventually(serviceProcess.Ready()).Should(BeClosed())
				services, err := consulClient.Agent().Services()
				Expect(err).NotTo(HaveOccurred())
				Expect(services).To(HaveLen(2))
				Expect(services[serviceName].Service).To(Equal(serviceName))
			})
		})
	})
})
