/*
 * Copyright (C) 2018 The "MysteriumNetwork/node" Authors.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package mysterium

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/mysteriumnetwork/node/identity/registry"
	"github.com/mysteriumnetwork/node/session/pingpong"
	pc "github.com/mysteriumnetwork/payments/crypto"
	"github.com/pkg/errors"

	"github.com/mysteriumnetwork/node/cmd"
	"github.com/mysteriumnetwork/node/consumer/statistics"
	"github.com/mysteriumnetwork/node/core/connection"
	"github.com/mysteriumnetwork/node/core/discovery"
	"github.com/mysteriumnetwork/node/core/ip"
	"github.com/mysteriumnetwork/node/core/location"
	"github.com/mysteriumnetwork/node/core/node"
	"github.com/mysteriumnetwork/node/eventbus"
	"github.com/mysteriumnetwork/node/feedback"
	"github.com/mysteriumnetwork/node/identity"
	"github.com/mysteriumnetwork/node/identity/selector"
	"github.com/mysteriumnetwork/node/logconfig"
	"github.com/mysteriumnetwork/node/market"
	"github.com/mysteriumnetwork/node/metadata"
	"github.com/mysteriumnetwork/node/services/openvpn"
	"github.com/mysteriumnetwork/node/services/wireguard"
	"github.com/rs/zerolog"
)

// MobileNode represents node object tuned for mobile devices
type MobileNode struct {
	shutdown                           func() error
	node                               *node.Node
	connectionManager                  connection.Manager
	locationResolver                   *location.Cache
	discoveryFinder                    *discovery.Finder
	identitySelector                   selector.Handler
	signerFactory                      identity.SignerFactory
	natPinger                          natPinger
	ipResolver                         ip.Resolver
	eventBus                           eventbus.EventBus
	connectionRegistry                 *connection.Registry
	statisticsTracker                  *statistics.SessionStatisticsTracker
	statisticsChangeCallback           StatisticsChangeCallback
	balanceChangeCallback              BalanceChangeCallback
	identityRegistrationChangeCallback IdentityRegistrationChangeCallback
	connectionStatusChangeCallback     ConnectionStatusChangeCallback
	proposalsManager                   *proposalsManager
	accountant                         identity.Identity
	feedbackReporter                   *feedback.Reporter
	transactor                         *registry.Transactor
	identityRegistry                   registry.IdentityRegistry
	consumerBalanceTracker             *pingpong.ConsumerBalanceTracker
	registryAddress                    string
	channelImplementationAddress       string
}

// MobileNetworkOptions alias for node.OptionsNetwork to be visible from mobile framework
type MobileNetworkOptions node.OptionsNetwork

// DefaultNetworkOptions returns default network options to connect with
func DefaultNetworkOptions() *MobileNetworkOptions {
	return &MobileNetworkOptions{
		Testnet:               true,
		ExperimentNATPunching: true,
		MysteriumAPIAddress:   metadata.TestnetDefinition.MysteriumAPIAddress,
		BrokerAddress:         metadata.TestnetDefinition.BrokerAddress,
		EtherClientRPC:        metadata.TestnetDefinition.EtherClientRPC,
	}
}

// NewNode function creates new Node
func NewNode(appPath string, optionsNetwork *MobileNetworkOptions) (*MobileNode, error) {
	var di cmd.Dependencies

	if appPath == "" {
		return nil, errors.New("node app path is required")
	}
	dataDir := filepath.Join(appPath, ".mysterium")
	currentDir := appPath

	network := node.OptionsNetwork(*optionsNetwork)
	logOptions := logconfig.LogOptions{
		LogLevel: zerolog.DebugLevel,
		LogHTTP:  false,
		Filepath: filepath.Join(dataDir, "mysterium-node"),
	}

	options := node.Options{
		LogOptions: logOptions,
		Directories: node.OptionsDirectory{
			Data:     dataDir,
			Storage:  filepath.Join(dataDir, "db"),
			Keystore: filepath.Join(dataDir, "keystore"),
			Runtime:  currentDir,
		},

		TequilapiEnabled: false,

		Openvpn: embeddedLibCheck{},

		Keystore: node.OptionsKeystore{
			UseLightweight: true,
		},
		FeedbackURL:    "https://feedback.mysterium.network",
		OptionsNetwork: network,
		Quality: node.OptionsQuality{
			Type:    node.QualityTypeMORQA,
			Address: "https://quality.mysterium.network/api/v1",
		},
		Discovery: node.OptionsDiscovery{
			Types:                  []node.DiscoveryType{node.DiscoveryTypeAPI, node.DiscoveryTypeBroker},
			Address:                network.MysteriumAPIAddress,
			ProposalFetcherEnabled: false,
		},
		Location: node.OptionsLocation{
			IPDetectorURL: "https://api.ipify.org/?format=json",
			Type:          node.LocationTypeOracle,
			Address:       "https://testnet-location.mysterium.network/api/v1/location",
		},
		Transactor: node.OptionsTransactor{
			TransactorEndpointAddress:       metadata.TestnetDefinition.TransactorAddress,
			RegistryAddress:                 metadata.TestnetDefinition.RegistryAddress,
			ChannelImplementation:           metadata.TestnetDefinition.ChannelImplAddress,
			ProviderMaxRegistrationAttempts: 10,
			ProviderRegistrationRetryDelay:  time.Minute * 3,
			ProviderRegistrationStake:       125000000000,
		},
		Accountant: node.OptionsAccountant{
			AccountantEndpointAddress: metadata.TestnetDefinition.AccountantAddress,
			AccountantID:              metadata.TestnetDefinition.AccountantID,
		},
		Payments: node.OptionsPayments{
			MaxAllowedPaymentPercentile:        1500,
			BCTimeout:                          time.Second * 30,
			AccountantPromiseSettlingThreshold: 0.1,
			SettlementTimeout:                  time.Hour * 2,
			MystSCAddress:                      "0x7753cfAD258eFbC52A9A1452e42fFbce9bE486cb",
		},
	}

	err := di.Bootstrap(options)
	if err != nil {
		return nil, errors.Wrap(err, "could not bootstrap dependencies")
	}

	mobileNode := &MobileNode{
		shutdown:                     func() error { return di.Shutdown() },
		node:                         di.Node,
		connectionManager:            di.ConnectionManager,
		locationResolver:             di.LocationResolver,
		discoveryFinder:              di.DiscoveryFinder,
		identitySelector:             di.IdentitySelector,
		signerFactory:                di.SignerFactory,
		natPinger:                    di.NATPinger,
		ipResolver:                   di.IPResolver,
		eventBus:                     di.EventBus,
		connectionRegistry:           di.ConnectionRegistry,
		statisticsTracker:            di.StatisticsTracker,
		accountant:                   identity.FromAddress(options.Accountant.AccountantID),
		feedbackReporter:             di.Reporter,
		transactor:                   di.Transactor,
		identityRegistry:             di.IdentityRegistry,
		consumerBalanceTracker:       di.ConsumerBalanceTracker,
		channelImplementationAddress: options.Transactor.ChannelImplementation,
		registryAddress:              options.Transactor.RegistryAddress,
		proposalsManager: newProposalsManager(
			di.DiscoveryFinder,
			di.ProposalStorage,
			di.MysteriumAPI,
			di.QualityClient,
		),
	}
	mobileNode.handleEvents()
	return mobileNode, nil
}

// GetProposals returns service proposals from API or cache. Proposals returned as JSON byte array since
// go mobile does not support complex slices.
func (mb *MobileNode) GetProposals(req *GetProposalsRequest) ([]byte, error) {
	return mb.proposalsManager.getProposals(req)
}

// GetProposal returns service proposal from cache.
func (mb *MobileNode) GetProposal(req *GetProposalRequest) ([]byte, error) {
	status := mb.connectionManager.Status()
	proposal, err := mb.proposalsManager.getProposal(req)
	if err != nil {
		return nil, errors.Wrap(err, "could not get proposal")
	}
	if proposal == nil {
		return nil, fmt.Errorf("proposal %s-%s not found", status.Proposal.ProviderID, status.Proposal.ServiceType)
	}
	return proposal, nil
}

// GetLocationResponse represents location response.
type GetLocationResponse struct {
	IP      string
	Country string
}

// GetLocation return current location including country and IP.
func (mb *MobileNode) GetLocation() (*GetLocationResponse, error) {
	loc, err := mb.locationResolver.DetectLocation()
	if err != nil {
		return nil, errors.Wrap(err, "could not get location")
	}

	return &GetLocationResponse{
		IP:      loc.IP,
		Country: loc.Country,
	}, nil
}

// GetStatusResponse represents status response.
type GetStatusResponse struct {
	State       string
	ProviderID  string
	ServiceType string
}

// GetStatus returns current connection state and provider info if connected to VPN.
func (mb *MobileNode) GetStatus() *GetStatusResponse {
	status := mb.connectionManager.Status()
	return &GetStatusResponse{
		State:       string(status.State),
		ProviderID:  status.Proposal.ProviderID,
		ServiceType: status.Proposal.ServiceType,
	}
}

// StatisticsChangeCallback represents statistics callback.
type StatisticsChangeCallback interface {
	OnChange(duration int64, bytesReceived int64, bytesSent int64)
}

// RegisterStatisticsChangeCallback registers callback which is called on active connection
// statistics change.
func (mb *MobileNode) RegisterStatisticsChangeCallback(cb StatisticsChangeCallback) {
	mb.statisticsChangeCallback = cb
}

// ConnectionStatusChangeCallback represents status callback.
type ConnectionStatusChangeCallback interface {
	OnChange(status string)
}

// RegisterConnectionStatusChangeCallback registers callback which is called on active connection
// status change.
func (mb *MobileNode) RegisterConnectionStatusChangeCallback(cb ConnectionStatusChangeCallback) {
	mb.connectionStatusChangeCallback = cb
}

// BalanceChangeCallback represents balance change callback.
type BalanceChangeCallback interface {
	OnChange(identityAddress string, balance int64)
}

// RegisterBalanceChangeCallback registers callback which is called on identity balance change.
func (mb *MobileNode) RegisterBalanceChangeCallback(cb BalanceChangeCallback) {
	mb.balanceChangeCallback = cb
}

// IdentityRegistrationChangeCallback represents identity registration status callback.
type IdentityRegistrationChangeCallback interface {
	OnChange(identityAddress string, status string)
}

// RegisterIdentityRegistrationChangeCallback registers callback which is called on identity registration status change.
func (mb *MobileNode) RegisterIdentityRegistrationChangeCallback(cb IdentityRegistrationChangeCallback) {
	mb.identityRegistrationChangeCallback = cb
}

// ConnectRequest represents connect request.
type ConnectRequest struct {
	IdentityAddress   string
	ProviderID        string
	ServiceType       string
	DisableKillSwitch bool
}

// Connect connects to given provider.
func (mb *MobileNode) Connect(req *ConnectRequest) error {
	proposal, err := mb.discoveryFinder.GetProposal(market.ProposalID{
		ProviderID:  req.ProviderID,
		ServiceType: req.ServiceType,
	})
	if err != nil {
		return errors.Wrap(err, "could not get proposal")
	}
	if proposal == nil {
		return fmt.Errorf("proposal %s-%s not found", req.ProviderID, req.ServiceType)
	}

	connectOptions := connection.ConnectParams{
		DisableKillSwitch: req.DisableKillSwitch,
		DNS:               connection.DNSOptionAuto,
	}
	if err := mb.connectionManager.Connect(identity.FromAddress(req.IdentityAddress), mb.accountant, *proposal, connectOptions); err != nil {
		return errors.Wrap(err, "could not connect")
	}
	return nil
}

// Disconnect disconnects or cancels current connection.
func (mb *MobileNode) Disconnect() error {
	if err := mb.connectionManager.Disconnect(); err != nil {
		return errors.Wrap(err, "could not disconnect")
	}
	return nil
}

// GetIdentityResponse represents identity response.
type GetIdentityResponse struct {
	IdentityAddress    string
	ChannelAddress     string
	RegistrationStatus string
}

// GetIdentity finds first identity and unlocks it.
// If there is no identity default one will be created.
func (mb *MobileNode) GetIdentity() (*GetIdentityResponse, error) {
	unlockedIdentity, err := mb.identitySelector.UseOrCreate("", "")
	if err != nil {
		return nil, errors.Wrap(err, "could not unlock identity")
	}

	channelAddress, err := pc.GenerateChannelAddress(unlockedIdentity.Address, metadata.TestnetDefinition.AccountantID, mb.registryAddress, mb.channelImplementationAddress)
	if err != nil {
		return nil, errors.Wrap(err, "could not generate channel address")
	}

	status, err := mb.identityRegistry.GetRegistrationStatus(identity.FromAddress(unlockedIdentity.Address))
	if err != nil {
		return nil, errors.Wrap(err, "could not get identity registration status")
	}

	return &GetIdentityResponse{
		IdentityAddress:    unlockedIdentity.Address,
		ChannelAddress:     channelAddress,
		RegistrationStatus: status.String(),
	}, nil
}

// GetIdentityRegistrationFeesResponse represents identity registration fees result.
type GetIdentityRegistrationFeesResponse struct {
	Fee int64
}

// GetIdentityRegistrationFees returns identity registration fees.
func (mb *MobileNode) GetIdentityRegistrationFees() (*GetIdentityRegistrationFeesResponse, error) {
	fees, err := mb.transactor.FetchRegistrationFees()
	if err != nil {
		return nil, errors.Wrap(err, "could not get registration fees")
	}
	return &GetIdentityRegistrationFeesResponse{Fee: int64(fees.Fee)}, nil
}

// RegisterIdentityRequest represents identity registration request.
type RegisterIdentityRequest struct {
	IdentityAddress string
	Fee             int64
}

// RegisterIdentity starts identity registration in background.
func (mb *MobileNode) RegisterIdentity(req *RegisterIdentityRequest) error {
	err := mb.transactor.RegisterIdentity(req.IdentityAddress, &registry.IdentityRegistrationRequestDTO{
		Stake:       0,
		Beneficiary: "",
		Fee:         uint64(req.Fee),
	})
	if err != nil {
		return errors.Wrap(err, "could not register identity")
	}
	return nil
}

// TopUpRequest represents top-up request.
type TopUpRequest struct {
	IdentityAddress string
}

// TopUp adds resets to default balance. This is temporary flow while
// payments are not production ready.
func (mb *MobileNode) TopUp(req *TopUpRequest) error {
	if err := mb.transactor.TopUp(req.IdentityAddress); err != nil {
		return errors.Wrap(err, "could not top-up balance")
	}
	return nil
}

// GetBalanceRequest represents balance request.
type GetBalanceRequest struct {
	IdentityAddress string
}

// GetBalanceResponse represents balance response.
type GetBalanceResponse struct {
	Balance int64
}

// GetBalance returns current balance.
func (mb *MobileNode) GetBalance(req *GetBalanceRequest) (*GetBalanceResponse, error) {
	balance := mb.consumerBalanceTracker.GetBalance(identity.FromAddress(req.IdentityAddress))
	return &GetBalanceResponse{Balance: int64(balance)}, nil
}

// SendFeedbackRequest represents user feedback request.
type SendFeedbackRequest struct {
	Description string
}

// SendFeedback sends user feedback via feedback reported.
func (mb *MobileNode) SendFeedback(req *SendFeedbackRequest) error {
	report := feedback.UserReport{
		Description: req.Description,
	}
	result, err := mb.feedbackReporter.NewIssue(report)
	if err != nil {
		return errors.Wrap(err, "could not create user report")
	}

	if !result.Success {
		return errors.New("user report sent but got error response")
	}
	return nil
}

// Shutdown function stops running mobile node
func (mb *MobileNode) Shutdown() error {
	return mb.shutdown()
}

// WaitUntilDies function returns when node stops
func (mb *MobileNode) WaitUntilDies() error {
	return mb.node.Wait()
}

// OverrideOpenvpnConnection replaces default openvpn connection factory with mobile related one returning session that can be reconnected
func (mb *MobileNode) OverrideOpenvpnConnection(tunnelSetup Openvpn3TunnelSetup) ReconnectableSession {
	openvpn.Bootstrap()

	st := &sessionTracker{}
	factory := &OpenvpnConnectionFactory{
		sessionTracker: st,
		signerFactory:  mb.signerFactory,
		tunnelSetup:    tunnelSetup,
		natPinger:      mb.natPinger,
		ipResolver:     mb.ipResolver,
	}
	_ = mb.eventBus.Subscribe(connection.StateEventTopic, st.handleState)
	mb.connectionRegistry.Register("openvpn", factory)
	return st
}

// OverrideWireguardConnection overrides default wireguard connection implementation to more mobile adapted one
func (mb *MobileNode) OverrideWireguardConnection(wgTunnelSetup WireguardTunnelSetup) {
	wireguard.Bootstrap()
	factory := &WireguardConnectionFactory{
		tunnelSetup: wgTunnelSetup,
	}
	mb.connectionRegistry.Register(wireguard.ServiceType, factory)
}

func (mb *MobileNode) handleEvents() {
	_ = mb.eventBus.SubscribeAsync(connection.StateEventTopic, func(e connection.StateEvent) {
		if mb.connectionStatusChangeCallback == nil {
			return
		}
		mb.connectionStatusChangeCallback.OnChange(string(e.State))
	})

	_ = mb.eventBus.SubscribeAsync(connection.StatisticsEventTopic, func(e connection.SessionStatsEvent) {
		if mb.statisticsChangeCallback == nil {
			return
		}

		duration := mb.statisticsTracker.GetSessionDuration()
		mb.statisticsChangeCallback.OnChange(int64(duration.Seconds()), int64(e.Stats.BytesReceived), int64(e.Stats.BytesSent))
	})

	_ = mb.eventBus.SubscribeAsync(pingpong.BalanceChangedTopic, func(e pingpong.BalanceChangedEvent) {
		if mb.balanceChangeCallback == nil {
			return
		}

		mb.balanceChangeCallback.OnChange(e.Identity.Address, int64(e.Current))
	})

	_ = mb.eventBus.SubscribeAsync(registry.RegistrationEventTopic, func(e registry.RegistrationEventPayload) {
		if mb.identityRegistrationChangeCallback == nil {
			return
		}

		mb.identityRegistrationChangeCallback.OnChange(e.ID.Address, e.Status.String())
	})
}
