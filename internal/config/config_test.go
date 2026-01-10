package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	content := `
server:
  http_addr: ":8080"
jwt:
  secret: "test"
github:
  app_id: 1
nomad:
  job_name: "test"
`
	tmp, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)
	defer func() {
		_ = os.Remove(tmp.Name())
	}()
	_, err = tmp.Write([]byte(content))
	require.NoError(t, err)
	err = tmp.Close()
	require.NoError(t, err)

	cfg, err := Load(tmp.Name())
	require.NoError(t, err)
	assert.Equal(t, ":8080", cfg.Server.HTTPAddr)
	assert.Equal(t, "test", cfg.JWT.Secret)
	assert.Equal(t, int64(1), cfg.GitHub.AppID)
}

func TestLoad_AdminAuthValidation(t *testing.T) {
	testCases := []struct {
		name               string
		content            string
		expectError        bool
		expectedUsername   string
		expectedPassword   string
		errorContains      string
	}{
		{
			name: "username only fails",
			content: `
jwt:
  secret: "test"
auth:
  admin_username: "user"
`,
			expectError:   true,
			errorContains: "auth.admin_username cannot be set without auth.admin_password",
		},
		{
			name: "password only defaults username",
			content: `
jwt:
  secret: "test"
auth:
  admin_password: "password"
`,
			expectError:      false,
			expectedUsername: "admin",
			expectedPassword: "password",
		},
		{
			name: "both set are accepted",
			content: `
jwt:
  secret: "test"
auth:
  admin_username: "custom_admin"
  admin_password: "password123"
`,
			expectError:      false,
			expectedUsername: "custom_admin",
			expectedPassword: "password123",
		},
		{
			name: "neither set is accepted",
			content: `
jwt:
  secret: "test"
`,
			expectError:      false,
			expectedUsername: "",
			expectedPassword: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmp, err := os.CreateTemp("", "config-*.yaml")
			require.NoError(t, err)
			defer func() {
				_ = os.Remove(tmp.Name())
			}()
			_, err = tmp.Write([]byte(tc.content))
			require.NoError(t, err)
			err = tmp.Close()
			require.NoError(t, err)

			cfg, err := Load(tmp.Name())

			if tc.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errorContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedUsername, cfg.Auth.AdminUsername)
				assert.Equal(t, tc.expectedPassword, cfg.Auth.AdminPassword)
			}
		})
	}
}
