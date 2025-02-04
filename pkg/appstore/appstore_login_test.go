package appstore

import (
	"encoding/json"
	"fmt"
	"github.com/golang/mock/gomock"
	"github.com/majd/ipatool/pkg/http"
	"github.com/majd/ipatool/pkg/keychain"
	"github.com/majd/ipatool/pkg/log"
	"github.com/majd/ipatool/pkg/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"os"
	"strings"
	"syscall"
)

var _ = Describe("AppStore (Login)", func() {
	const (
		testPassword = "test-password"
	)

	var (
		ctrl         *gomock.Controller
		as           AppStore
		mockKeychain *keychain.MockKeychain
		mockClient   *http.MockClient[LoginResult]
		mockMachine  *util.MockMachine
		mockLogger   *log.MockLogger
		testErr      = errors.New("test")
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		mockClient = http.NewMockClient[LoginResult](ctrl)
		mockMachine = util.NewMockMachine(ctrl)
		mockLogger = log.NewMockLogger(ctrl)
		as = &appstore{
			keychain:    mockKeychain,
			loginClient: mockClient,
			ioReader:    os.Stdin,
			machine:     mockMachine,
			logger:      mockLogger,
			interactive: true,
		}

		mockLogger.EXPECT().
			Verbose().
			Return(nil).
			MaxTimes(4)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("not running in interactive mode and password is not supplied", func() {
		BeforeEach(func() {
			as.(*appstore).interactive = false
		})

		It("returns error", func() {
			err := as.Login("", "", "")
			Expect(err).To(MatchError(ContainSubstring("password is required")))
		})
	})

	When("prompts user for password", func() {
		BeforeEach(func() {
			mockLogger.EXPECT().
				Log().
				Return(nil)
		})

		When("user enters password", func() {
			BeforeEach(func() {
				mockMachine.EXPECT().
					MacAddress().
					Return("", errors.New("success"))

				mockMachine.EXPECT().
					ReadPassword(syscall.Stdin).
					Return([]byte(testPassword), nil)
			})

			It("succeeds", func() {
				err := as.Login("", "", "")
				Expect(err).To(MatchError(ContainSubstring("success")))
			})
		})

		When("fails to read password from stdin", func() {
			BeforeEach(func() {
				mockMachine.EXPECT().
					ReadPassword(syscall.Stdin).
					Return(nil, testErr)
			})

			It("returns error", func() {
				err := as.Login("", "", "")
				Expect(err).To(MatchError(ContainSubstring(ErrGetData.Error())))
			})
		})
	})

	When("fails to read Machine's MAC address", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", testErr)
		})

		It("returns error", func() {
			err := as.Login("", testPassword, "")
			Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
			Expect(err).To(MatchError(ContainSubstring(ErrGetMAC.Error())))
		})
	})

	When("sucessfully reads Machine's MAC address", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)
		})

		When("client returns error", func() {
			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[LoginResult]{}, testErr)
			})

			It("returns wrapped error", func() {
				err := as.Login("", testPassword, "")
				Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
			})
		})

		When("store API returns invalid first response", func() {
			const testCustomerMessage = "test"

			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[LoginResult]{
						Data: LoginResult{
							FailureType:     FailureTypeInvalidCredentials,
							CustomerMessage: "test",
						},
					}, nil).
					Times(2)
			})

			It("retries one more time", func() {
				err := as.Login("", testPassword, "")
				Expect(err).To(MatchError(ContainSubstring(testCustomerMessage)))
			})
		})

		When("store API returns error", func() {
			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[LoginResult]{
						Data: LoginResult{
							FailureType: "random-error",
						},
					}, nil)
			})

			It("returns error", func() {
				err := as.Login("", testPassword, "")
				Expect(err).To(MatchError(ContainSubstring(ErrGeneric.Error())))
			})
		})

		When("store API requires 2FA code", func() {
			When("not running in interactive mode", func() {
				BeforeEach(func() {
					as.(*appstore).interactive = false

					mockLogger.EXPECT().
						Log().
						Return(nil).
						Times(2)

					mockClient.EXPECT().
						Send(gomock.Any()).
						Return(http.Result[LoginResult]{
							Data: LoginResult{
								FailureType:     "",
								CustomerMessage: CustomerMessageBadLogin,
							},
						}, nil)
				})

				It("prints message", func() {
					err := as.Login("", testPassword, "")
					Expect(err).ToNot(HaveOccurred())
				})
			})

			When("user enters 2FA code", func() {
				BeforeEach(func() {
					mockLogger.EXPECT().
						Log().
						Return(nil).
						Times(2)

					mockKeychain.EXPECT().
						Set("account", gomock.Any()).
						Return(nil)

					mockClient.EXPECT().
						Send(gomock.Any()).
						Return(http.Result[LoginResult]{
							Data: LoginResult{
								FailureType:     "",
								CustomerMessage: CustomerMessageBadLogin,
							},
						}, nil).
						Times(2)

					as.(*appstore).ioReader = strings.NewReader("123456\n")
				})

				It("successfully authenticates", func() {
					err := as.Login("", testPassword, "")
					Expect(err).ToNot(HaveOccurred())
				})
			})

			When("prompts user for 2FA code", func() {
				BeforeEach(func() {
					mockLogger.EXPECT().
						Log().
						Return(nil)

					mockClient.EXPECT().
						Send(gomock.Any()).
						Return(http.Result[LoginResult]{
							Data: LoginResult{
								FailureType:     "",
								CustomerMessage: CustomerMessageBadLogin,
							},
						}, nil)

					as.(*appstore).ioReader = strings.NewReader("123456")
				})

				It("fails to read 2FA code from stdin", func() {
					err := as.Login("", testPassword, "")
					Expect(err).To(MatchError(ContainSubstring(ErrGetData.Error())))
				})
			})
		})

		When("store API returns valid response", func() {
			const (
				testEmail               = "test-email"
				testFirstName           = "test-first-name"
				testLastName            = "test-last-name"
				testPasswordToken       = "test-password-token"
				testDirectoryServicesID = "directory-services-id"
			)

			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[LoginResult]{
						Data: LoginResult{
							PasswordToken:       testPasswordToken,
							DirectoryServicesID: testDirectoryServicesID,
							Account: LoginAccountResult{
								Email: testEmail,
								Address: LoginAddressResult{
									FirstName: testFirstName,
									LastName:  testLastName,
								},
							},
						},
					}, nil)
			})

			When("fails to save account in keychain", func() {
				BeforeEach(func() {
					mockKeychain.EXPECT().
						Set("account", gomock.Any()).
						Do(func(key string, data []byte) {
							want := Account{
								Name:                fmt.Sprintf("%s %s", testFirstName, testLastName),
								Email:               testEmail,
								PasswordToken:       testPasswordToken,
								Password:            testPassword,
								DirectoryServicesID: testDirectoryServicesID,
							}

							var got Account
							err := json.Unmarshal(data, &got)
							Expect(err).ToNot(HaveOccurred())
							Expect(got).To(Equal(want))
						}).
						Return(testErr)
				})

				It("returns error", func() {
					err := as.Login("", testPassword, "")
					Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
					Expect(err).To(MatchError(ContainSubstring(ErrSetKeychainItem.Error())))
				})
			})

			When("sucessfully saves account in keychain", func() {
				BeforeEach(func() {
					mockLogger.EXPECT().
						Log().
						Return(nil)

					mockKeychain.EXPECT().
						Set("account", gomock.Any()).
						Do(func(key string, data []byte) {
							want := Account{
								Name:                fmt.Sprintf("%s %s", testFirstName, testLastName),
								Email:               testEmail,
								PasswordToken:       testPasswordToken,
								Password:            testPassword,
								DirectoryServicesID: testDirectoryServicesID,
							}

							var got Account
							err := json.Unmarshal(data, &got)
							Expect(err).ToNot(HaveOccurred())
							Expect(got).To(Equal(want))
						}).
						Return(nil)
				})

				It("returns nil", func() {
					err := as.Login("", testPassword, "")
					Expect(err).ToNot(HaveOccurred())
				})
			})
		})
	})
})
