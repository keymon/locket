package locket_test

import (
	"strings"

	"github.com/cloudfoundry-incubator/locket"
	"github.com/hashicorp/consul/api"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Service", func() {
	var (
		apiClient      *api.Client
		logger         *lagertest.TestLogger
		serviceRunner  locket.Service
		serviceProcess ifrit.Process
	)

	BeforeEach(func() {
		apiClient = consulRunner.NewClient()
		logger = lagertest.NewTestLogger("locket")
	})

	JustBeforeEach(func() {
		clock := clock.NewClock()
		serviceRunner = locket.NewService(apiClient, logger, "testservice", 8000, "0.0.0.0", clock)
	})

	AfterEach(func() {
		ginkgomon.Kill(serviceProcess)
	})

	checkService := func(client *api.Client) {
		var testService *api.AgentService
		Eventually(func() *api.AgentService {
			var err error
			services, err := apiClient.Agent().Services()
			Expect(err).NotTo(HaveOccurred())
			testService = services["testservice"]
			return testService
		}).ShouldNot(BeNil())

		Expect(testService.Service).To(Equal("testservice"))
		Expect(testService.ID).To(Equal("testservice"))
		Expect(testService.Port).To(Equal(8000))
		Expect(testService.Address).To(Equal("0.0.0.0"))
	}

	Context("When consul is running", func() {
		It("signals ready", func() {
			serviceProcess = ifrit.Background(serviceRunner)
			Eventually(serviceProcess.Ready()).Should(BeClosed())
		})

		It("registers the service", func() {
			serviceProcess = ifrit.Background(serviceRunner)
			checkService(apiClient)
		})

		It("registers forever while consul is running", func() {
			serviceProcess = ifrit.Background(serviceRunner)
			Consistently(serviceProcess.Wait()).ShouldNot(Receive())
		})

		Context("and the process is signalled", func() {
			It("deregisters the service", func() {
				serviceProcess = ifrit.Background(serviceRunner)
				checkService(apiClient)
				ginkgomon.Kill(serviceProcess)
				Eventually(func() *api.AgentService {
					services, err := apiClient.Agent().Services()
					Expect(err).NotTo(HaveOccurred())
					return services["testservice"]
				}).Should(BeNil())
				Consistently(func() *api.AgentService {
					services, err := apiClient.Agent().Services()
					Expect(err).NotTo(HaveOccurred())
					return services["testservice"]
				}).Should(BeNil())
			})
		})
	})

	Context("When consul isn't running", func() {
		BeforeEach(func() {
			consulRunner.Stop()
		})

		JustBeforeEach(func() {
			serviceProcess = ifrit.Background(serviceRunner)
			Eventually(serviceProcess.Ready()).Should(BeClosed())
		})

		Context("When consul starts up", func() {
			It("registers the service", func() {
				_, err := apiClient.Agent().Services()
				Expect(err).To(HaveOccurred())

				consulRunner.Start()
				checkService(apiClient)
			})
		})

		It("Continously tries to register the serivce", func() {
			Eventually(func() int {
				logs := logger.LogMessages()
				count := 0
				for _, message := range logs {
					if strings.Contains(message, "try-register-service") {
						count++
					}
				}
				return count
			}).Should(BeNumerically(">", 2))
		})
	})
})
