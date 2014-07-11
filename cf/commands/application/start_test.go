package application_test

import (
	"os"
	"time"

	testapi "github.com/cloudfoundry/cli/cf/api/fakes"
	"github.com/cloudfoundry/cli/cf/configuration"
	"github.com/cloudfoundry/cli/cf/errors"
	"github.com/cloudfoundry/cli/cf/models"
	clock "github.com/cloudfoundry/cli/clock/fakes"
	testcmd "github.com/cloudfoundry/cli/testhelpers/commands"
	testconfig "github.com/cloudfoundry/cli/testhelpers/configuration"
	testlogs "github.com/cloudfoundry/cli/testhelpers/logs"
	testreq "github.com/cloudfoundry/cli/testhelpers/requirements"
	testterm "github.com/cloudfoundry/cli/testhelpers/terminal"
	"github.com/cloudfoundry/loggregatorlib/logmessage"

	. "github.com/cloudfoundry/cli/cf/commands/application"
	. "github.com/cloudfoundry/cli/testhelpers/matchers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = FDescribe("start command", func() {
	var (
		ui                        *testterm.FakeUI
		cmd                       *Start
		defaultAppForStart        = models.Application{}
		defaultInstanceReponses   = [][]models.AppInstanceFields{}
		defaultInstanceErrorCodes = []string{"", ""}
		requirementsFactory       *testreq.FakeReqFactory
		configRepo                configuration.ReadWriter
		appRepo                   *testapi.FakeApplicationRepository
		appDisplayer              *testcmd.FakeAppDisplayer
		appInstancesRepo          *testapi.FakeAppInstancesRepo
		logRepo                   *testapi.FakeLogsRepository

		clockDestroyer chan bool
	)

	mockClock := &clock.FakeClock{}

	BeforeEach(func() {
		ui = &testterm.FakeUI{}
		clockDestroyer = make(chan bool, 1)
		requirementsFactory = &testreq.FakeReqFactory{}
		configRepo = testconfig.NewRepositoryWithDefaults()
		appDisplayer = &testcmd.FakeAppDisplayer{}
		appRepo = &testapi.FakeApplicationRepository{}
		appInstancesRepo = &testapi.FakeAppInstancesRepo{}
		logRepo = &testapi.FakeLogsRepository{}

		defaultAppForStart.Name = "my-app"
		defaultAppForStart.Guid = "my-app-guid"
		defaultAppForStart.InstanceCount = 2

		domain := models.DomainFields{}
		domain.Name = "example.com"

		route := models.RouteSummary{}
		route.Host = "my-app"
		route.Domain = domain

		defaultAppForStart.Routes = []models.RouteSummary{route}

		starting := models.AppInstanceFields{State: models.InstanceStarting}
		running := models.AppInstanceFields{State: models.InstanceRunning}

		defaultInstanceReponses = [][]models.AppInstanceFields{
			[]models.AppInstanceFields{starting, starting},
			[]models.AppInstanceFields{starting, starting},
			[]models.AppInstanceFields{starting, running},
		}

		cmd = NewStart(ui, configRepo, mockClock, appDisplayer, appRepo, appInstancesRepo, logRepo)
	})

	runCommand := func(args ...string) {
		testcmd.RunCommand(cmd, args, requirementsFactory)
	}

	runTheClock := func(stopChannel chan bool) {
		for {
			select {
			case <-time.After(time.Millisecond * 100):
				mockClock.Tick()
			case <-stopChannel:
				return
			}
		}
	}

	BeforeEach(func() {
		go runTheClock(clockDestroyer)
	})

	AfterEach(func() {
		clockDestroyer <- true
	})

	// FIXME: KILL THIS FUNCTION
	startAppWithInstancesAndErrors := func(app models.Application, instances [][]models.AppInstanceFields, errorCodes []string) {
		appRepo.UpdateAppResult = app
		appRepo.ReadReturns.App = app
		requirementsFactory.Application = app
		appInstancesRepo.GetInstancesResponses = instances
		appInstancesRepo.GetInstancesErrorCodes = errorCodes

		logRepo.TailLogMessages = []*logmessage.LogMessage{
			testlogs.NewLogMessage("Log Line 1", app.Guid, LogMessageTypeStaging, time.Now()),
			testlogs.NewLogMessage("Log Line 2", app.Guid, LogMessageTypeStaging, time.Now()),
		}

		runCommand("my-app")
		return
	}

	Describe("requirements", func() {
		It("fails requirements when not logged in", func() {
			runCommand("some-app-name")
			Expect(testcmd.CommandDidPassRequirements).To(BeFalse())
		})

		It("fails with usage when provided with no args", func() {
			runCommand()
			Expect(ui.FailedWithUsage).To(BeTrue())
		})
	})

	Describe("timeouts", func() {
		BeforeEach(func() {
			app := defaultAppForStart
			appRepo.UpdateAppResult = app
			appRepo.ReadReturns.App = app
			requirementsFactory.Application = app
			requirementsFactory.LoginSuccess = true
		})

		It("has sane default timeout values", func() {
			Expect(cmd.StagingTimeout).To(Equal(15 * time.Minute))
			Expect(cmd.StartupTimeout).To(Equal(5 * time.Minute))
		})

		It("can read timeout values from environment variables", func() {
			oldStaging := os.Getenv("CF_STAGING_TIMEOUT")
			oldStart := os.Getenv("CF_STARTUP_TIMEOUT")
			defer func() {
				os.Setenv("CF_STAGING_TIMEOUT", oldStaging)
				os.Setenv("CF_STARTUP_TIMEOUT", oldStart)
			}()

			os.Setenv("CF_STAGING_TIMEOUT", "6")
			os.Setenv("CF_STARTUP_TIMEOUT", "3")

			cmd = NewStart(ui, configRepo, mockClock, appDisplayer, appRepo, appInstancesRepo, logRepo)
			Expect(cmd.StagingTimeout).To(Equal(6 * time.Minute))
			Expect(cmd.StartupTimeout).To(Equal(3 * time.Minute))
		})

		Describe("when the staging timeout is zero seconds", func() {
			BeforeEach(func() {
				cmd.StagingTimeout = 0
				cmd.PingerThrottle = 1
				cmd.StartupTimeout = 1
			})

			It("can still respond to staging failures", func() {
				appInstancesRepo.GetInstancesErrorCodes = []string{"170001"}
				testcmd.RunCommand(cmd, []string{"my-app"}, requirementsFactory)

				Expect(ui.Outputs).To(ContainSubstrings(
					[]string{"my-app"},
					[]string{"OK"},
					[]string{"FAILED"},
					[]string{"Error staging app"},
				))
			})
		})
	})

	Context("when logged in", func() {
		BeforeEach(func() {
			requirementsFactory.LoginSuccess = true
			cmd.StagingTimeout = 50 * time.Millisecond
			cmd.StartupTimeout = 50 * time.Millisecond
			cmd.PingerThrottle = 50 * time.Millisecond
		})

		Context("when an app with the given name exists", func() {
			BeforeEach(func() {
				appRepo.UpdateAppResult = defaultAppForStart
				appRepo.ReadReturns.App = defaultAppForStart
				requirementsFactory.Application = defaultAppForStart
				appInstancesRepo.GetInstancesResponses = defaultInstanceReponses
				appInstancesRepo.GetInstancesErrorCodes = defaultInstanceErrorCodes

				currentTime := time.Now()
				wrongSourceName := "DEA"
				correctSourceName := "STG"
				logRepo.TailLogMessages = []*logmessage.LogMessage{
					testlogs.NewLogMessage("Log Line 1", defaultAppForStart.Guid, wrongSourceName, currentTime),
					testlogs.NewLogMessage("Log Line 2", defaultAppForStart.Guid, correctSourceName, currentTime),
					testlogs.NewLogMessage("Log Line 3", defaultAppForStart.Guid, correctSourceName, currentTime),
					testlogs.NewLogMessage("Log Line 4", defaultAppForStart.Guid, wrongSourceName, currentTime),
				}
			})

			It("starts an app, when given the app's name", func() {
				runCommand("my-app")

				Expect(ui.Outputs).To(ContainSubstrings(
					[]string{"my-app", "my-org", "my-space", "my-user"},
					[]string{"OK"},
					[]string{"0 of 2 instances running", "2 starting"},
					[]string{"started"},
				))

				Expect(requirementsFactory.ApplicationName).To(Equal("my-app"))
				Expect(appRepo.UpdateAppGuid).To(Equal("my-app-guid"))
				Expect(appDisplayer.AppToDisplay).To(Equal(defaultAppForStart))
			})

			It("only displays staging logs when an app is starting", func() {
				runCommand("my-app")

				Expect(ui.Outputs).To(ContainSubstrings(
					[]string{"Log Line 2"},
					[]string{"Log Line 3"},
				))
				Expect(ui.Outputs).ToNot(ContainSubstrings(
					[]string{"Log Line 1"},
					[]string{"Log Line 4"},
				))
			})

			Context("when the app is still staging", func() {
				BeforeEach(func() {
					down := models.AppInstanceFields{State: models.InstanceDown}
					starting := models.AppInstanceFields{State: models.InstanceStarting}
					running := models.AppInstanceFields{State: models.InstanceRunning}

					appInstancesRepo.GetInstancesResponses = [][]models.AppInstanceFields{
						[]models.AppInstanceFields{},
						[]models.AppInstanceFields{},
						[]models.AppInstanceFields{down, starting},
						[]models.AppInstanceFields{starting, starting},
						[]models.AppInstanceFields{running, running},
					}
					appInstancesRepo.GetInstancesErrorCodes = []string{
						errors.APP_NOT_STAGED,
						errors.APP_NOT_STAGED,
						"", "", "",
					}

					logRepo.TailLogMessages = []*logmessage.LogMessage{
						testlogs.NewLogMessage("Log Line 1", defaultAppForStart.Guid, LogMessageTypeStaging, time.Now()),
						testlogs.NewLogMessage("Log Line 2", defaultAppForStart.Guid, LogMessageTypeStaging, time.Now()),
					}

				})

				It("gracefully handles starting the app", func() {
					runCommand("my-app")

					Expect(appInstancesRepo.GetInstancesAppGuid).To(Equal("my-app-guid"))
					Expect(ui.Outputs).To(ContainSubstrings(
						[]string{"Log Line 1"},
						[]string{"Log Line 2"},
						[]string{"0 of 2 instances running", "2 starting"},
					))
				})
			})

			Context("when staging the app fails", func() {
				BeforeEach(func() {
					appInstancesRepo.GetInstancesResponses = [][]models.AppInstanceFields{}
					appInstancesRepo.GetInstancesErrorCodes = []string{"170001"}
				})

				It("displays an error message when staging fails", func() {
					runCommand("my-app")

					Expect(ui.Outputs).To(ContainSubstrings(
						[]string{"FAILED"},
						[]string{"Error staging app"},
					))
				})
			})

			Context("when an app instance is flapping", func() {
				It("fails and alerts the user", func() {
					appInstance := models.AppInstanceFields{}
					appInstance.State = models.InstanceStarting
					appInstance2 := models.AppInstanceFields{}
					appInstance2.State = models.InstanceStarting
					appInstance3 := models.AppInstanceFields{}
					appInstance3.State = models.InstanceStarting
					appInstance4 := models.AppInstanceFields{}
					appInstance4.State = models.InstanceFlapping
					instances := [][]models.AppInstanceFields{
						[]models.AppInstanceFields{appInstance, appInstance2},
						[]models.AppInstanceFields{appInstance3, appInstance4},
					}

					errorCodes := []string{"", ""}

					startAppWithInstancesAndErrors(defaultAppForStart, instances, errorCodes)

					Expect(ui.Outputs).To(ContainSubstrings(
						[]string{"my-app"},
						[]string{"OK"},
						[]string{"0 of 2 instances running", "1 starting", "1 failing"},
						[]string{"FAILED"},
						[]string{"Start unsuccessful"},
					))
				})
			})

			Context("when waiting for the app to start times out", func() {
				BeforeEach(func() {
					down := models.AppInstanceFields{State: models.InstanceDown}
					starting := models.AppInstanceFields{State: models.InstanceStarting}

					appInstancesRepo.GetInstancesResponses = [][]models.AppInstanceFields{
						[]models.AppInstanceFields{starting, starting},
						[]models.AppInstanceFields{starting, down},
						[]models.AppInstanceFields{down, down},
					}
					appInstancesRepo.GetInstancesErrorCodes = []string{
						errors.APP_NOT_STAGED,
						errors.APP_NOT_STAGED,
						errors.APP_NOT_STAGED,
					}
				})

				It("fails tells the user about it", func() {
					runCommand("my-app")

					Expect(ui.Outputs).To(ContainSubstrings(
						[]string{"FAILED"},
						[]string{"Start app timeout"},
					))
					Expect(ui.Outputs).ToNot(ContainSubstrings([]string{"instances running"}))
				})
			})
		})

		It("tells the user about the failure when starting the app fails", func() {
			app := models.Application{}
			app.Name = "my-app"
			app.Guid = "my-app-guid"

			appRepo.UpdateErr = true
			appRepo.ReadReturns.App = app
			requirementsFactory.Application = app

			runCommand("my-app")
			Expect(ui.Outputs).To(ContainSubstrings(
				[]string{"my-app"},
				[]string{"FAILED"},
				[]string{"Error updating app."},
			))
			Expect(appRepo.UpdateAppGuid).To(Equal("my-app-guid"))
		})

		It("warns the user when the app is already running", func() {
			app := models.Application{}
			app.Name = "my-app"
			app.Guid = "my-app-guid"
			app.State = "started"

			appRepo.ReadReturns.App = app
			requirementsFactory.Application = app

			runCommand("my-app")

			Expect(ui.Outputs).To(ContainSubstrings([]string{"my-app", "is already started"}))
			Expect(appRepo.UpdateAppGuid).To(Equal(""))
		})

		It("tells the user when connecting to the log server fails", func() {
			appRepo.ReadReturns.App = defaultAppForStart
			appInstancesRepo.GetInstancesResponses = defaultInstanceReponses
			appInstancesRepo.GetInstancesErrorCodes = defaultInstanceErrorCodes

			logRepo.TailLogErr = errors.New("Ooops")
			requirementsFactory.Application = defaultAppForStart

			runCommand("my-app")

			Expect(ui.Outputs).To(ContainSubstrings(
				[]string{"error tailing logs"},
				[]string{"Ooops"},
			))
		})
	})
})
