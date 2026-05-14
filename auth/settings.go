package auth

type Settings struct {
	RestAPIURL             string `json:"restApiUrl"`
	HealthAPIURL           string `json:"healthApiUrl"`
	SSORedirectURL         string `json:"ssoRedirectUrl"`
	SSOEnv                 string `json:"ssoEnv"`
	SSOUserMgmtURL         string `json:"ssoUserMgmtUrl"`
	SSOResetPwdURL         string `json:"ssoResetPwdUrl"`
	SSOResetPwdCallbackURL string `json:"ssoResetPwdCallbackUrl"`
	SSOChangePwdURL        string `json:"ssoChangePwdUrl"`
	SSOAdminTokenURL       string `json:"ssoAdminTokenUrl"`
	SSOAdminTokenAuth      string `json:"ssoAdminTokenAuth"`
	SSOBaseURL             string `json:"ssoBaseUrl"`
	SSOEndpointAuthN       string `json:"ssoEndpointAuthN"`
	SSOAuthNContentType    string `json:"ssoAuthNContentType"`
	SSOAuthNTokenName      string `json:"ssoAuthNTokenName"`
	SSOEndpointAuthZ       string `json:"ssoEndpointAuthZ"`
	SSOEndpointTokens      string `json:"ssoEndpointTokens"`
	SSOClientIDAuthN       string `json:"ssoClientIdAuthN"`
	SSOClientIDAuthZ       string `json:"ssoClientIdAuthZ"`
	SSOFQDN                string `json:"ssoFqdn"`
}

type AuthToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Success      bool   `json:"success,omitempty"`
}
