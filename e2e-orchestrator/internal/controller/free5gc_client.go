/*
Copyright 2024 The Nephio Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Default values matching free5GC webconsole defaults
const (
	DefaultVar5qi        = 9
	DefaultPriorityLevel = 8
	DefaultPlmnID        = "20893"
	DefaultDNN           = "internet"
)

// Free5GCClient provides methods to interact with the free5GC WebConsole.
type Free5GCClient struct {
	BaseURL    string
	Username   string
	Password   string
	token      string
	httpClient *http.Client
}

// NewFree5GCClient creates a new Free5GC WebConsole client.
func NewFree5GCClient(baseURL, username, password string) *Free5GCClient {
	return &Free5GCClient{
		BaseURL:  baseURL,
		Username: username,
		Password: password,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// LoginRequest is the request body for login API.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is the response from login API.
type LoginResponse struct {
	AccessToken string `json:"access_token"`
}

// Login authenticates with the free5GC WebConsole and stores the token.
func (c *Free5GCClient) Login() error {
	loginReq := LoginRequest{
		Username: c.Username,
		Password: c.Password,
	}

	body, err := json.Marshal(loginReq)
	if err != nil {
		return fmt.Errorf("failed to marshal login request: %w", err)
	}

	req, err := http.NewRequest("POST", c.BaseURL+"/api/login", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var loginResp LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return fmt.Errorf("failed to decode login response: %w", err)
	}

	c.token = loginResp.AccessToken
	fmt.Printf("[free5GC] Login successful\n")
	return nil
}

// ---- Subscriber Data Structures (matching webconsole API) ----

// SubsData is the main subscriber data structure for the webconsole API.
type SubsData struct {
	PlmnID                            string                              `json:"plmnID"`
	UeId                              string                              `json:"ueId"`
	AuthenticationSubscription        WebAuthenticationSubscription       `json:"AuthenticationSubscription"`
	AccessAndMobilitySubscriptionData AccessAndMobilitySubscriptionData   `json:"AccessAndMobilitySubscriptionData"`
	SessionManagementSubscriptionData []SessionManagementSubscriptionData `json:"SessionManagementSubscriptionData"`
	SmfSelectionSubscriptionData      SmfSelectionSubscriptionData        `json:"SmfSelectionSubscriptionData"`
	AmPolicyData                      AmPolicyData                        `json:"AmPolicyData"`
	SmPolicyData                      SmPolicyData                        `json:"SmPolicyData"`
}

// WebAuthenticationSubscription contains UE authentication info.
type WebAuthenticationSubscription struct {
	AuthenticationMethod          string        `json:"authenticationMethod"`
	PermanentKey                  *PermanentKey `json:"permanentKey"`
	SequenceNumber                string        `json:"sequenceNumber"`
	Milenage                      *Milenage     `json:"milenage,omitempty"`
	Opc                           *Opc          `json:"opc,omitempty"`
	AuthenticationManagementField string        `json:"authenticationManagementField,omitempty"`
}

// PermanentKey is the permanent key for UE authentication.
type PermanentKey struct {
	PermanentKeyValue   string `json:"permanentKeyValue"`
	EncryptionKey       int32  `json:"encryptionKey"`
	EncryptionAlgorithm int32  `json:"encryptionAlgorithm"`
}

// Milenage contains MILENAGE algorithm parameters.
type Milenage struct {
	Op *Op `json:"op,omitempty"`
}

// Op is the operator key.
type Op struct {
	OpValue             string `json:"opValue"`
	EncryptionKey       int32  `json:"encryptionKey"`
	EncryptionAlgorithm int32  `json:"encryptionAlgorithm"`
}

// Opc is the derived operator key.
type Opc struct {
	OpcValue            string `json:"opcValue"`
	EncryptionKey       int32  `json:"encryptionKey"`
	EncryptionAlgorithm int32  `json:"encryptionAlgorithm"`
}

// AccessAndMobilitySubscriptionData contains AM subscription data.
type AccessAndMobilitySubscriptionData struct {
	Gpsis            []string `json:"gpsis"`
	Nssai            *Nssai   `json:"nssai,omitempty"`
	SubscribedUeAmbr *AmbrRm  `json:"subscribedUeAmbr,omitempty"`
}

// Nssai contains network slice selection info.
type Nssai struct {
	DefaultSingleNssais []Snssai `json:"defaultSingleNssais,omitempty"`
	SingleNssais        []Snssai `json:"singleNssais,omitempty"`
}

// Snssai is a Single NSSAI.
type Snssai struct {
	Sst int32  `json:"sst"`
	Sd  string `json:"sd,omitempty"`
}

// AmbrRm is the subscribed UE AMBR.
type AmbrRm struct {
	Downlink string `json:"downlink"`
	Uplink   string `json:"uplink"`
}

// SessionManagementSubscriptionData contains SM subscription data.
type SessionManagementSubscriptionData struct {
	SingleNssai       *Snssai                     `json:"singleNssai,omitempty"`
	DnnConfigurations map[string]DnnConfiguration `json:"dnnConfigurations,omitempty"`
}

// DnnConfiguration is the DNN configuration.
type DnnConfiguration struct {
	PduSessionTypes *PduSessionTypes      `json:"pduSessionTypes,omitempty"`
	SscModes        *SscModes             `json:"sscModes,omitempty"`
	SessionAmbr     *Ambr                 `json:"sessionAmbr,omitempty"`
	Var5gQosProfile *SubscribedDefaultQos `json:"5gQosProfile,omitempty"`
}

// PduSessionTypes contains PDU session type info.
type PduSessionTypes struct {
	DefaultSessionType  string   `json:"defaultSessionType"`
	AllowedSessionTypes []string `json:"allowedSessionTypes,omitempty"`
}

// SscModes contains SSC mode info.
type SscModes struct {
	DefaultSscMode  string   `json:"defaultSscMode"`
	AllowedSscModes []string `json:"allowedSscModes,omitempty"`
}

// Ambr is the session AMBR.
type Ambr struct {
	Downlink string `json:"downlink"`
	Uplink   string `json:"uplink"`
}

// SubscribedDefaultQos is the default QoS profile.
type SubscribedDefaultQos struct {
	Var5qi        int   `json:"5qi"`
	Arp           *Arp  `json:"arp,omitempty"`
	PriorityLevel int32 `json:"priorityLevel,omitempty"`
}

// Arp is the allocation and retention priority.
type Arp struct {
	PriorityLevel int32 `json:"priorityLevel"`
}

// SmfSelectionSubscriptionData contains SMF selection data.
type SmfSelectionSubscriptionData struct {
	SubscribedSnssaiInfos map[string]SnssaiInfo `json:"subscribedSnssaiInfos,omitempty"`
}

// SnssaiInfo contains SNSSAI info.
type SnssaiInfo struct {
	DnnInfos []DnnInfo `json:"dnnInfos,omitempty"`
}

// DnnInfo contains DNN info.
type DnnInfo struct {
	Dnn string `json:"dnn"`
}

// AmPolicyData contains AM policy data.
type AmPolicyData struct {
	SubscCats []string `json:"subscCats,omitempty"`
}

// SmPolicyData contains SM policy data.
type SmPolicyData struct {
	SmPolicySnssaiData map[string]SmPolicySnssaiData `json:"smPolicySnssaiData,omitempty"`
}

// SmPolicySnssaiData contains SM policy SNSSAI data.
type SmPolicySnssaiData struct {
	Snssai          *Snssai                    `json:"snssai,omitempty"`
	SmPolicyDnnData map[string]SmPolicyDnnData `json:"smPolicyDnnData,omitempty"`
}

// SmPolicyDnnData contains SM policy DNN data.
type SmPolicyDnnData struct {
	Dnn string `json:"dnn"`
}

// ---- Helper Functions ----

// sdToHexString converts SD (decimal uint32) to hex string format (e.g., 66051 -> "010203").
func sdToHexString(sd uint32) string {
	return fmt.Sprintf("%06x", sd)
}

// snssaiKey generates the SNSSAI key for maps (e.g., "01010203" for SST=1, SD="010203").
func snssaiKey(sst int32, sdHex string) string {
	return fmt.Sprintf("%02d%s", sst, sdHex)
}

// buildDefaultSubsData builds the default subscriber data based on free5GC webconsole sample,
// with customized IMSI and 5QI values.
// The slice is added to DefaultSingleNssais to ensure "Default S-NSSAI" is enabled.
func buildDefaultSubsData(imsi string, fiveQI int, sst uint32, sd uint32) *SubsData {
	sdHex := sdToHexString(sd)
	sstInt := int32(sst)

	// Format IMSI with prefix
	ueId := fmt.Sprintf("imsi-%s", imsi)

	// Generate unique GPSI based on IMSI to avoid duplicate gpsi error
	// Use last 10 digits of IMSI as MSISDN
	msisdn := imsi
	if len(msisdn) > 10 {
		msisdn = msisdn[len(msisdn)-10:]
	}
	gpsi := fmt.Sprintf("msisdn-%s", msisdn)

	subsData := &SubsData{
		PlmnID: DefaultPlmnID,
		UeId:   ueId,
		AuthenticationSubscription: WebAuthenticationSubscription{
			AuthenticationManagementField: "8000",
			AuthenticationMethod:          "5G_AKA",
			// Use OPc (not OP) - Milenage Op is left empty
			Opc: &Opc{
				EncryptionAlgorithm: 0,
				EncryptionKey:       0,
				OpcValue:            "8e27b6af0e692e750f32667a3b14605d",
			},
			PermanentKey: &PermanentKey{
				EncryptionAlgorithm: 0,
				EncryptionKey:       0,
				PermanentKeyValue:   "8baf473f2f8fd09487cccbd7097c6862",
			},
			SequenceNumber: "000000000023",
		},
		AccessAndMobilitySubscriptionData: AccessAndMobilitySubscriptionData{
			// Each UE needs unique GPSI to avoid "duplicate gpsi" error on update
			Gpsis: []string{gpsi},
			Nssai: &Nssai{
				// Adding slice to DefaultSingleNssais ensures "Default S-NSSAI" is checked
				DefaultSingleNssais: []Snssai{
					{Sst: sstInt, Sd: sdHex},
				},
				SingleNssais: []Snssai{
					{Sst: sstInt, Sd: sdHex},
				},
			},
			SubscribedUeAmbr: &AmbrRm{
				Downlink: "1000 Kbps",
				Uplink:   "1000 Kbps",
			},
		},
		SessionManagementSubscriptionData: []SessionManagementSubscriptionData{
			{
				SingleNssai: &Snssai{
					Sst: sstInt,
					Sd:  sdHex,
				},
				DnnConfigurations: map[string]DnnConfiguration{
					DefaultDNN: {
						PduSessionTypes: &PduSessionTypes{
							DefaultSessionType:  "IPV4",
							AllowedSessionTypes: []string{"IPV4"},
						},
						SscModes: &SscModes{
							DefaultSscMode:  "SSC_MODE_1",
							AllowedSscModes: []string{"SSC_MODE_1"},
						},
						SessionAmbr: &Ambr{
							Downlink: "1000 Kbps",
							Uplink:   "1000 Kbps",
						},
						Var5gQosProfile: &SubscribedDefaultQos{
							Var5qi: fiveQI,
							Arp: &Arp{
								PriorityLevel: DefaultPriorityLevel,
							},
							PriorityLevel: DefaultPriorityLevel,
						},
					},
				},
			},
		},
		SmfSelectionSubscriptionData: SmfSelectionSubscriptionData{
			SubscribedSnssaiInfos: map[string]SnssaiInfo{
				snssaiKey(sstInt, sdHex): {
					DnnInfos: []DnnInfo{
						{Dnn: DefaultDNN},
					},
				},
			},
		},
		AmPolicyData: AmPolicyData{
			SubscCats: []string{"free5gc"},
		},
		SmPolicyData: SmPolicyData{
			SmPolicySnssaiData: map[string]SmPolicySnssaiData{
				snssaiKey(sstInt, sdHex): {
					Snssai: &Snssai{
						Sst: sstInt,
						Sd:  sdHex,
					},
					SmPolicyDnnData: map[string]SmPolicyDnnData{
						DefaultDNN: {Dnn: DefaultDNN},
					},
				},
			},
		},
	}

	return subsData
}

// RegisterSubscriber registers a new subscriber in free5GC with the specified parameters.
// imsi: UE IMSI (e.g., "208930000000001")
// fiveQI: 5G QoS Identifier
// sst: Slice/Service Type
// sd: Slice Differentiator (decimal)
func (c *Free5GCClient) RegisterSubscriber(imsi string, fiveQI int, sst uint32, sd uint32) error {
	// Ensure we have a valid token
	if c.token == "" {
		if err := c.Login(); err != nil {
			return fmt.Errorf("failed to login: %w", err)
		}
	}

	// Build subscriber data
	subsData := buildDefaultSubsData(imsi, fiveQI, sst, sd)

	body, err := json.Marshal(subsData)
	if err != nil {
		return fmt.Errorf("failed to marshal subscriber data: %w", err)
	}

	// API endpoint: POST /api/subscriber/{ueId}/{servingPlmnId}
	url := fmt.Sprintf("%s/api/subscriber/imsi-%s/%s", c.BaseURL, imsi, DefaultPlmnID)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle response
	if resp.StatusCode == http.StatusCreated {
		fmt.Printf("[free5GC] Successfully registered UE %s with 5QI=%d, SST=%d, SD=%d\n",
			imsi, fiveQI, sst, sd)
		return nil
	}

	// Handle conflict (UE already exists)
	if resp.StatusCode == http.StatusConflict {
		fmt.Printf("[free5GC] UE %s already exists, attempting update\n", imsi)
		return c.UpdateSubscriberQoS(imsi, fiveQI, sst, sd)
	}

	// Handle unauthorized (token expired)
	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Printf("[free5GC] Token expired, re-authenticating\n")
		c.token = ""
		if err := c.Login(); err != nil {
			return fmt.Errorf("re-authentication failed: %w", err)
		}
		// Retry with new token
		return c.RegisterSubscriber(imsi, fiveQI, sst, sd)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("registration failed with status %d: %s", resp.StatusCode, string(bodyBytes))
}

// UpdateSubscriberQoS updates the QoS profile for an existing subscriber.
func (c *Free5GCClient) UpdateSubscriberQoS(imsi string, fiveQI int, sst uint32, sd uint32) error {
	// Ensure we have a valid token
	if c.token == "" {
		if err := c.Login(); err != nil {
			return fmt.Errorf("failed to login: %w", err)
		}
	}

	// Build subscriber data (same as registration, but PUT instead of POST)
	subsData := buildDefaultSubsData(imsi, fiveQI, sst, sd)

	body, err := json.Marshal(subsData)
	if err != nil {
		return fmt.Errorf("failed to marshal subscriber data: %w", err)
	}

	// API endpoint: PUT /api/subscriber/{ueId}/{servingPlmnId}
	url := fmt.Sprintf("%s/api/subscriber/imsi-%s/%s", c.BaseURL, imsi, DefaultPlmnID)

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		fmt.Printf("[free5GC] Successfully updated UE %s with 5QI=%d, SST=%d, SD=%d\n",
			imsi, fiveQI, sst, sd)
		return nil
	}

	// Handle unauthorized (token expired)
	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Printf("[free5GC] Token expired, re-authenticating\n")
		c.token = ""
		if err := c.Login(); err != nil {
			return fmt.Errorf("re-authentication failed: %w", err)
		}
		// Retry with new token
		return c.UpdateSubscriberQoS(imsi, fiveQI, sst, sd)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("update failed with status %d: %s", resp.StatusCode, string(bodyBytes))
}

// DeleteSubscriber removes a subscriber from free5GC.
func (c *Free5GCClient) DeleteSubscriber(imsi string) error {
	// Ensure we have a valid token
	if c.token == "" {
		if err := c.Login(); err != nil {
			return fmt.Errorf("failed to login: %w", err)
		}
	}

	// API endpoint: DELETE /api/subscriber/{ueId}/{servingPlmnId}
	url := fmt.Sprintf("%s/api/subscriber/imsi-%s/%s", c.BaseURL, imsi, DefaultPlmnID)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		fmt.Printf("[free5GC] Successfully deleted UE %s\n", imsi)
		return nil
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("delete failed with status %d: %s", resp.StatusCode, string(bodyBytes))
}
