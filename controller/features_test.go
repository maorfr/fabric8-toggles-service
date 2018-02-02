package controller_test

import (
	"context"
	"crypto/rsa"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"reflect"
	"testing"

	unleashapi "github.com/Unleash/unleash-client-go/api"
	jwt "github.com/dgrijalva/jwt-go"
	jwtrequest "github.com/dgrijalva/jwt-go/request"
	"github.com/dnaeon/go-vcr/cassette"
	"github.com/dnaeon/go-vcr/recorder"
	"github.com/fabric8-services/fabric8-auth/log"
	"github.com/fabric8-services/fabric8-toggles-service/app"
	"github.com/fabric8-services/fabric8-toggles-service/app/test"
	"github.com/fabric8-services/fabric8-toggles-service/controller"
	"github.com/fabric8-services/fabric8-toggles-service/featuretoggles"
	"github.com/goadesign/goa"
	goajwt "github.com/goadesign/goa/middleware/security/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var disabledFeature, singleStrategyFeature, multiStrategiesFeature, releasedFeature unleashapi.Feature

func init() {
	// features
	disabledFeature = unleashapi.Feature{
		Name:        "disabledFeature",
		Description: "Disabled feature",
		Enabled:     false,
		Strategies:  []unleashapi.Strategy{},
	}

	singleStrategyFeature = unleashapi.Feature{
		Name:        "singleStrategyFeature",
		Description: "Feature with single strategy",
		Enabled:     true,
		Strategies: []unleashapi.Strategy{
			{
				Name: featuretoggles.EnableByLevelStrategyName,
				Parameters: map[string]interface{}{
					featuretoggles.LevelParameter: featuretoggles.InternalLevel,
				},
			},
		},
	}

	multiStrategiesFeature = unleashapi.Feature{
		Name:        "multiStrategiesFeature",
		Description: "Feature with multiple strategies",
		Enabled:     true,
		Strategies: []unleashapi.Strategy{
			{
				Name: featuretoggles.EnableByLevelStrategyName,
				Parameters: map[string]interface{}{
					featuretoggles.LevelParameter: featuretoggles.InternalLevel,
				},
			},
			{
				Name: featuretoggles.EnableByLevelStrategyName,
				Parameters: map[string]interface{}{
					featuretoggles.LevelParameter: featuretoggles.ExperimentalLevel,
				},
			},
			{
				Name: featuretoggles.EnableByLevelStrategyName,
				Parameters: map[string]interface{}{
					featuretoggles.LevelParameter: featuretoggles.BetaLevel,
				},
			},
		},
	}

	releasedFeature = unleashapi.Feature{
		Name:        "releasedFeature",
		Description: "Feature released",
		Enabled:     true,
		Strategies: []unleashapi.Strategy{
			{
				Name: featuretoggles.EnableByLevelStrategyName,
				Parameters: map[string]interface{}{
					featuretoggles.LevelParameter: featuretoggles.ReleasedLevel,
				},
			},
		},
	}
}

// MockTogglesClient a mock of the toggles client
type MockTogglesClient struct {
}

func (m *MockTogglesClient) IsFeatureEnabled(ctx context.Context, feature unleashapi.Feature, userLevel string) bool {
	if reflect.DeepEqual(feature, disabledFeature) {
		return false
	}
	if reflect.DeepEqual(feature, multiStrategiesFeature) && userLevel == featuretoggles.ExperimentalLevel {
		return true
	}
	if reflect.DeepEqual(feature, multiStrategiesFeature) && userLevel == featuretoggles.BetaLevel {
		return true
	}
	if reflect.DeepEqual(feature, releasedFeature) {
		return true
	}

	return false
}

func (m *MockTogglesClient) GetFeatures(ctx context.Context, names []string) []*unleashapi.Feature {
	if reflect.DeepEqual(names, []string{disabledFeature.Name, multiStrategiesFeature.Name}) {
		return []*unleashapi.Feature{&disabledFeature, &multiStrategiesFeature}
	} else if reflect.DeepEqual(names, []string{releasedFeature.Name, disabledFeature.Name, multiStrategiesFeature.Name}) {
		return []*unleashapi.Feature{&releasedFeature, &disabledFeature, &multiStrategiesFeature}
	}
	return nil
}

func (m *MockTogglesClient) GetFeature(name string) *unleashapi.Feature {
	switch name {
	case disabledFeature.Name:
		return &disabledFeature
	case singleStrategyFeature.Name:
		return &singleStrategyFeature
	case multiStrategiesFeature.Name:
		return &multiStrategiesFeature
	case releasedFeature.Name:
		return &releasedFeature
	default:
		return nil
	}
}

func (m *MockTogglesClient) Close() error {
	return nil
}

type TestFeatureControllerConfig struct {
	authServiceURL string
}

func (c *TestFeatureControllerConfig) GetAuthServiceURL() string {
	return c.authServiceURL
}

func NewFeaturesController(httpClient *http.Client, togglesClient featuretoggles.Client) (*goa.Service, *controller.FeaturesController) {
	svc := goa.New("feature")
	ctrl := controller.NewFeaturesController(svc,
		// featuretoggles.NewClientWithState(unleashClient, true),
		&MockTogglesClient{},
		httpClient,
		&TestFeatureControllerConfig{
			authServiceURL: "http://auth",
		},
	)
	return svc, ctrl
}

func TestShowFeature(t *testing.T) {
	// given
	r := newRecorder(t, "../test/data/controller/auth_get_user")
	defer r.Stop()
	svc, ctrl := NewFeaturesController(&http.Client{Transport: r.Transport}, &MockTogglesClient{})

	t.Run("fail", func(t *testing.T) {
		t.Run("not found", func(t *testing.T) {
			// when/then
			userID := "user_beta_level"
			test.ShowFeaturesNotFound(t, createValidContext(t, &userID), svc, ctrl, "UnknownFeature")
		})
	})

	t.Run("disabled for user", func(t *testing.T) {

		t.Run("disabled for all", func(t *testing.T) {
			// when
			userID := "user_beta_level"
			_, appFeature := test.ShowFeaturesOK(t, createValidContext(t, &userID), svc, ctrl, disabledFeature.Name)
			// then
			require.NotNil(t, appFeature)
			expectedFeatureData := &app.Feature{
				ID:   disabledFeature.Name,
				Type: "features",
				Attributes: &app.FeatureAttributes{
					Description:     disabledFeature.Description,
					Enabled:         false,
					UserEnabled:     false,
					EnablementLevel: nil, // feature is disabled, hence no opt-in level would work anyways
				},
			}
			assert.Equal(t, expectedFeatureData, appFeature.Data)
		})

		t.Run("user with no level", func(t *testing.T) {
			// when
			userID := "user_no_level"
			_, appFeature := test.ShowFeaturesOK(t, createValidContext(t, &userID), svc, ctrl, singleStrategyFeature.Name)
			// then
			require.NotNil(t, appFeature)
			expectedFeatureData := &app.Feature{
				ID:   singleStrategyFeature.Name,
				Type: "features",
				Attributes: &app.FeatureAttributes{
					Description:     singleStrategyFeature.Description,
					Enabled:         true,
					UserEnabled:     false,
					EnablementLevel: nil, // because the feature level is internal but the user is external
				},
			}
			assert.Equal(t, expectedFeatureData, appFeature.Data)
		})

	})

	t.Run("enabled for user", func(t *testing.T) {

		t.Run("unreleased feature", func(t *testing.T) {

			t.Run("user with enough level", func(t *testing.T) {

				t.Run("experimental user", func(t *testing.T) {
					// when
					userID := "user_experimental_level"
					_, appFeature := test.ShowFeaturesOK(t, createValidContext(t, &userID), svc, ctrl, multiStrategiesFeature.Name)
					// then
					require.NotNil(t, appFeature)
					enablementLevel := featuretoggles.BetaLevel
					expectedFeatureData := &app.Feature{
						ID:   multiStrategiesFeature.Name,
						Type: "features",
						Attributes: &app.FeatureAttributes{
							Description:     multiStrategiesFeature.Description,
							Enabled:         true,
							UserEnabled:     true,
							EnablementLevel: &enablementLevel,
						},
					}
					assert.Equal(t, expectedFeatureData, appFeature.Data)
				})
			})
		})

		t.Run("released feature", func(t *testing.T) {

			t.Run("user with no level", func(t *testing.T) {
				// when
				userID := "user_no_level"
				_, appFeature := test.ShowFeaturesOK(t, createValidContext(t, &userID), svc, ctrl, releasedFeature.Name)
				// then
				require.NotNil(t, appFeature)
				enablementLevel := featuretoggles.ReleasedLevel
				expectedFeatureData := &app.Feature{
					ID:   releasedFeature.Name,
					Type: "features",
					Attributes: &app.FeatureAttributes{
						Description:     releasedFeature.Description,
						Enabled:         true,
						UserEnabled:     true,
						EnablementLevel: &enablementLevel,
					},
				}
				assert.Equal(t, expectedFeatureData, appFeature.Data)
			})

			t.Run("user with enough level", func(t *testing.T) {

				t.Run("experimental level", func(t *testing.T) {
					// when
					userID := "user_experimental_level"
					_, appFeature := test.ShowFeaturesOK(t, createValidContext(t, &userID), svc, ctrl, releasedFeature.Name)
					// then
					require.NotNil(t, appFeature)
					enablementLevel := featuretoggles.ReleasedLevel
					expectedFeatureData := &app.Feature{
						ID:   releasedFeature.Name,
						Type: "features",
						Attributes: &app.FeatureAttributes{
							Description:     releasedFeature.Description,
							Enabled:         true,
							UserEnabled:     true,
							EnablementLevel: &enablementLevel,
						},
					}
					assert.Equal(t, expectedFeatureData, appFeature.Data)
				})

				t.Run("released level", func(t *testing.T) {
					// when
					userID := "user_released_level"
					_, appFeature := test.ShowFeaturesOK(t, createValidContext(t, &userID), svc, ctrl, releasedFeature.Name)
					// then
					require.NotNil(t, appFeature)
					enablementLevel := featuretoggles.ReleasedLevel
					expectedFeatureData := &app.Feature{
						ID:   releasedFeature.Name,
						Type: "features",
						Attributes: &app.FeatureAttributes{
							Description:     releasedFeature.Description,
							Enabled:         true,
							UserEnabled:     true,
							EnablementLevel: &enablementLevel,
						},
					}
					assert.Equal(t, expectedFeatureData, appFeature.Data)
				})

				t.Run("nopreproduction level", func(t *testing.T) {
					// when
					userID := "user_nopreproduction_level"
					_, appFeature := test.ShowFeaturesOK(t, createValidContext(t, &userID), svc, ctrl, releasedFeature.Name)
					// then
					require.NotNil(t, appFeature)
					enablementLevel := featuretoggles.ReleasedLevel
					expectedFeatureData := &app.Feature{
						ID:   releasedFeature.Name,
						Type: "features",
						Attributes: &app.FeatureAttributes{
							Description:     releasedFeature.Description,
							Enabled:         true,
							UserEnabled:     true,
							EnablementLevel: &enablementLevel,
						},
					}
					assert.Equal(t, expectedFeatureData, appFeature.Data)
				})
			})
		})
	})
}

func TestListFeatures(t *testing.T) {

	// given
	r := newRecorder(t, "../test/data/controller/auth_get_user")
	defer r.Stop()
	svc, ctrl := NewFeaturesController(&http.Client{Transport: r.Transport}, &MockTogglesClient{})

	t.Run("ok", func(t *testing.T) {
		t.Run("2 matches", func(t *testing.T) {
			// when
			userID := "user_beta_level"
			_, featuresList := test.ListFeaturesOK(t, createValidContext(t, &userID), svc, ctrl, []string{disabledFeature.Name, multiStrategiesFeature.Name})
			// then
			experimentalLevel := featuretoggles.BetaLevel
			expectedData := []*app.Feature{
				{
					ID:   disabledFeature.Name,
					Type: "features",
					Attributes: &app.FeatureAttributes{
						Description:     disabledFeature.Description,
						Enabled:         false,
						UserEnabled:     false,
						EnablementLevel: nil,
					},
				},
				{
					ID:   multiStrategiesFeature.Name,
					Type: "features",
					Attributes: &app.FeatureAttributes{
						Description:     multiStrategiesFeature.Description,
						Enabled:         true,
						UserEnabled:     true,
						EnablementLevel: &experimentalLevel,
					},
				},
			}
			assert.Equal(t, expectedData, featuresList.Data)
		})

		t.Run("no feature found", func(t *testing.T) {
			// when
			userID := "user_beta_level"
			_, featuresList := test.ListFeaturesOK(t, createValidContext(t, &userID), svc, ctrl, []string{"FeatureX", "FeatureY", "FeatureZ"})
			// then
			expectedData := []*app.Feature{}
			assert.Equal(t, expectedData, featuresList.Data)
		})

		t.Run("no user provided", func(t *testing.T) {
			// when
			_, featuresList := test.ListFeaturesOK(t, createValidContext(t, nil), svc, ctrl, []string{"FeatureX", "FeatureY", "FeatureZ"})
			// then
			expectedData := []*app.Feature{}
			assert.Equal(t, expectedData, featuresList.Data)
		})
		t.Run("no user provided only released features matches", func(t *testing.T) {
			// when
			_, featuresList := test.ListFeaturesOK(t, createValidContext(t, nil), svc, ctrl, []string{releasedFeature.Name, disabledFeature.Name, multiStrategiesFeature.Name})
			// then
			releasedLevel := featuretoggles.ReleasedLevel
			betaLevel := featuretoggles.BetaLevel
			expectedData := []*app.Feature{
				{
					ID:   releasedFeature.Name,
					Type: "features",
					Attributes: &app.FeatureAttributes{
						Description:     releasedFeature.Description,
						Enabled:         true,
						UserEnabled:     true,
						EnablementLevel: &releasedLevel,
					},
				},
				{
					ID:   disabledFeature.Name,
					Type: "features",
					Attributes: &app.FeatureAttributes{
						Description:     disabledFeature.Description,
						Enabled:         false,
						UserEnabled:     false,
						EnablementLevel: nil,
					},
				},
				{
					ID:   multiStrategiesFeature.Name,
					Type: "features",
					Attributes: &app.FeatureAttributes{
						Description:     multiStrategiesFeature.Description,
						Enabled:         true,
						UserEnabled:     false,
						EnablementLevel: &betaLevel,
					},
				},
			}
			assert.Equal(t, expectedData, featuresList.Data)
		})
	})

}

func JWTMatcher() cassette.Matcher {
	return func(httpRequest *http.Request, cassetteRequest cassette.Request) bool {
		// look-up the JWT's "sub" claim and compare with the request
		token, err := jwtrequest.ParseFromRequest(httpRequest, jwtrequest.AuthorizationHeaderExtractor, func(*jwt.Token) (interface{}, error) {
			return PublicKey()
		})
		if err != nil {
			log.Panic(nil, map[string]interface{}{"error": err.Error()}, "failed to parse token from request")
		}
		claims := token.Claims.(jwt.MapClaims)
		if sub, found := cassetteRequest.Headers["sub"]; found {
			return sub[0] == claims["sub"]
		}
		return false
	}
}

func createValidContext(t *testing.T, userID *string) context.Context {
	claims := jwt.MapClaims{}
	var token *jwt.Token = nil
	if userID != nil {
		claims["sub"] = *userID
		token = jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		// use the test private key to sign the token
		key, err := PrivateKey()
		require.NoError(t, err)
		signed, err := token.SignedString(key)
		require.NoError(t, err)
		token.Raw = signed
	}
	return goajwt.WithJWT(context.Background(), token)
}

func newRecorder(t *testing.T, cassetteName string) *recorder.Recorder {
	_, err := os.Stat(fmt.Sprintf("%s.yaml", cassetteName))
	require.NoError(t, err)
	r, err := recorder.New(cassetteName)
	require.NoError(t, err)
	_, err = PublicKey()
	require.NoError(t, err)
	// custom cassette matcher that will compare the HTTP requests' token subject with the `sub` header of the recorded data (the yaml file)
	r.SetMatcher(JWTMatcher())
	return r
}

func PrivateKey() (*rsa.PrivateKey, error) {
	rsaPrivateKey, err := ioutil.ReadFile("../test/private_key.pem")
	if err != nil {
		return nil, err
	}
	return jwt.ParseRSAPrivateKeyFromPEM(rsaPrivateKey)
}

func PublicKey() (*rsa.PublicKey, error) {
	rsaPublicKey, err := ioutil.ReadFile("../test/public_key.pem")
	if err != nil {
		return nil, err
	}
	return jwt.ParseRSAPublicKeyFromPEM(rsaPublicKey)
}
