package hyperliquid

import (
	"fmt"
	"math"

	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// IExchangeAPI is an interface for the /exchange service.
type IExchangeAPI interface {
	IClient

	// Open orders
	BulkOrders(requests []OrderRequest, grouping Grouping) (*PlaceOrderResponse, error)
	Order(request OrderRequest, grouping Grouping) (*PlaceOrderResponse, error)
	MarketOrder(coin string, size float64, slippage *float64) (*PlaceOrderResponse, error)
	LimitOrder(orderType string, coin string, size float64, px float64, isBuy bool, reduceOnly bool) (*PlaceOrderResponse, error)
	TakeProfitOrder(coin string, size float64, triggerPx float64, isBuy bool) (*PlaceOrderResponse, error)
	StopLossOrder(coin string, size float64, triggerPx float64, isBuy bool) (*PlaceOrderResponse, error)
	// Order management
	CancelOrderByOID(coin string, orderID int) (any, error)
	BulkCancelOrders(cancels []CancelOidWire) (any, error)
	CancelAllOrdersByCoin(coin string) (any, error)
	CancelAllOrders() (any, error)
	ClosePosition(coin string) (*PlaceOrderResponse, error)

	// Account management
	Withdraw(destination string, amount float64) (*WithdrawResponse, error)
	UpdateLeverage(coin string, isCross bool, leverage int) (any, error)
	TransferUSDCSpotVsPerp(amount float64, toPerp bool) (*TransferUSDCSpotPerpResponse, error)
	Transfer(amount float64, toPerp bool) (*TransferUSDCSpotPerpResponse, error)
}

// Implement the IExchangeAPI interface.
type ExchangeAPI struct {
	Client
	infoAPI      *InfoAPI
	address      string
	baseEndpoint string
	meta         map[string]AssetInfo
}

// NewExchangeAPI creates a new default ExchangeAPI.
// Run SetPrivateKey() and SetAccountAddress() to set the private key and account address.
func NewExchangeAPI(isMainnet bool) *ExchangeAPI {
	api := ExchangeAPI{
		Client:       *NewClient(isMainnet),
		baseEndpoint: "/exchange",
		infoAPI:      NewInfoAPI(isMainnet),
		address:      "",
	}
	// turn on debug mode if there is an error with /info service
	meta, err := api.infoAPI.BuildMetaMap()
	if err != nil {
		api.SetDebugActive()
		api.debug("Error building meta map: %s", err)
	}
	api.meta = meta
	return &api
}

func (api *ExchangeAPI) Endpoint() string {
	return api.baseEndpoint
}

// Helper function to calculate the slippage price based on the market price.
func (api *ExchangeAPI) SlippagePrice(coin string, isBuy bool, slippage float64) float64 {
	marketPx, err := api.infoAPI.GetMartketPx(coin)
	if err != nil {
		api.debug("Error getting market price: %s", err)
		return 0.0
	}
	return CalculateSlippage(isBuy, marketPx, slippage)
}

// Open a market order.
// Limit order with TIF=IOC and px=market price * (1 +- slippage).
// Size determines the amount of the coin to buy/sell.
//
//	MarketOrder("BTC", 0.1, nil) // Buy 0.1 BTC
//	MarketOrder("BTC", -0.1, nil) // Sell 0.1 BTC
//	MarketOrder("BTC", 0.1, &slippage) // Buy 0.1 BTC with slippage
func (api *ExchangeAPI) MarketOrder(coin string, size float64, slippage *float64) (*PlaceOrderResponse, error) {
	slpg := GetSlippage(slippage)
	isBuy := IsBuy(size)
	finalPx := api.SlippagePrice(coin, isBuy, slpg)
	orderType := OrderType{
		Limit: &LimitOrderType{
			Tif: TifIoc,
		},
	}
	orderRequest := OrderRequest{
		Coin:       coin,
		IsBuy:      isBuy,
		Sz:         math.Abs(size),
		LimitPx:    finalPx,
		OrderType:  orderType,
		ReduceOnly: false,
	}
	return api.Order(orderRequest, GroupingNa)
}

// Open a limit order.
// Order type can be Gtc, Ioc, Alo.
// Size determines the amount of the coin to buy/sell.
// See the constants TifGtc, TifIoc, TifAlo.
func (api *ExchangeAPI) LimitOrder(orderType string, coin string, size float64, px float64, reduceOnly bool) (*PlaceOrderResponse, error) {
	// check if the order type is valid
	if orderType != TifGtc && orderType != TifIoc && orderType != TifAlo {
		return nil, APIError{Message: fmt.Sprintf("Invalid order type: %s. Available types: %s, %s, %s", orderType, TifGtc, TifIoc, TifAlo)}
	}
	orderTypeZ := OrderType{
		Limit: &LimitOrderType{
			Tif: orderType,
		},
	}
	orderRequest := OrderRequest{
		Coin:       coin,
		IsBuy:      IsBuy(size),
		Sz:         math.Abs(size),
		LimitPx:    px,
		OrderType:  orderTypeZ,
		ReduceOnly: reduceOnly,
	}
	return api.Order(orderRequest, GroupingNa)
}

// Open a take profit order.
// Size determines the amount of the coin to buy/sell.
func (api *ExchangeAPI) TakeProfitOrder(coin string, size float64, triggerPx float64, isBuy bool) (*TpSlOrderResponse, error) {
	// check if the order type is valid
	slpg := GetSlippage(nil)
	finalPx := api.SlippagePrice(coin, isBuy, slpg)
	info := api.meta[coin]

	orderTypeZ := OrderType{
		Trigger: &TriggerOrderType{
			IsMarket:  true,
			TpSl:      TriggerTp,
			TriggerPx: FloatToWire(triggerPx, &info.SzDecimals),
		},
	}
	orderRequest := OrderRequest{
		Coin:       coin,
		IsBuy:      isBuy,
		Sz:         math.Abs(size),
		OrderType:  orderTypeZ,
		LimitPx:    finalPx,
		ReduceOnly: true,
	}
	return api.TpSlOrder(orderRequest, GroupingTpSl)
}

// Open a stop loss order.
// Size determines the amount of the coin to buy/sell.
func (api *ExchangeAPI) StopLossOrder(coin string, size float64, triggerPx float64, isBuy bool) (*TpSlOrderResponse, error) {
	// check if the order type is valid
	slpg := GetSlippage(nil)
	finalPx := api.SlippagePrice(coin, isBuy, slpg)
	info := api.meta[coin]
	orderTypeZ := OrderType{
		Trigger: &TriggerOrderType{
			IsMarket:  true,
			TpSl:      TriggerSl,
			TriggerPx: FloatToWire(triggerPx, &info.SzDecimals),
		},
	}
	orderRequest := OrderRequest{
		Coin:       coin,
		IsBuy:      isBuy,
		Sz:         math.Abs(size),
		OrderType:  orderTypeZ,
		LimitPx:    finalPx,
		ReduceOnly: true,
	}
	return api.TpSlOrder(orderRequest, GroupingTpSl)
}

// Close all positions for a given coin. They are closing with a market order.
func (api *ExchangeAPI) ClosePosition(coin string) (*PlaceOrderResponse, error) {
	// Get all positions and find the one for the coin
	// Then just make MarketOpen with the reverse size
	state, err := api.infoAPI.GetUserState(api.AccountAddress())
	if err != nil {
		api.debug("Error GetUserState: %s", err)
		return nil, err
	}
	positions := state.AssetPositions
	slippage := GetSlippage(nil)

	// Find the position for the coin
	for _, position := range positions {
		item := position.Position
		if coin != item.Coin {
			continue
		}
		size := item.Szi
		// reverse the position to close
		isBuy := !IsBuy(size)
		finalPx := api.SlippagePrice(coin, isBuy, slippage)
		orderType := OrderType{
			Limit: &LimitOrderType{
				Tif: "Ioc",
			},
		}
		orderRequest := OrderRequest{
			Coin:       coin,
			IsBuy:      isBuy,
			Sz:         math.Abs(size),
			LimitPx:    finalPx,
			OrderType:  orderType,
			ReduceOnly: true,
		}
		return api.Order(orderRequest, GroupingNa)
	}
	return nil, APIError{Message: fmt.Sprintf("No position found for %s", coin)}
}

// Place single order
func (api *ExchangeAPI) Order(request OrderRequest, grouping Grouping) (*PlaceOrderResponse, error) {
	return api.BulkOrders([]OrderRequest{request}, grouping)
}

// Place limit order
func (api *ExchangeAPI) TpSlOrder(request OrderRequest, grouping Grouping) (*TpSlOrderResponse, error) {
	return makeBulkOrdersGeneric[TpSlOrderResponse](api, []OrderRequest{request}, grouping)
}

// Place orders in bulk
// https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint#place-an-order
func (api *ExchangeAPI) BulkOrders(requests []OrderRequest, grouping Grouping) (*PlaceOrderResponse, error) {
	return makeBulkOrdersGeneric[PlaceOrderResponse](api, requests, grouping)
}

func makeBulkOrdersGeneric[T any](api *ExchangeAPI, requests []OrderRequest, grouping Grouping) (*T, error) {
	var wires []OrderWire
	for _, req := range requests {
		wires = append(wires, OrderRequestToWire(req, api.meta))
	}
	timestamp := GetNonce()
	action := OrderWiresToOrderAction(wires, grouping)
	v, r, s, err := api.SignL1Action(action, timestamp)
	if err != nil {
		api.debug("Error signing L1 action: %s", err)
		return nil, err
	}
	request := ExchangeRequest{
		Action:       action,
		Nonce:        timestamp,
		Signature:    ToTypedSig(r, s, v),
		VaultAddress: nil,
	}
	return MakeUniversalRequest[T](api, request)
}

// Cancel order(s)
// https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint#cancel-order-s
func (api *ExchangeAPI) BulkCancelOrders(cancels []CancelOidWire) (*CancelOrderResponse, error) {
	timestamp := GetNonce()
	action := CancelOidOrderAction{
		Type:    "cancel",
		Cancels: cancels,
	}
	v, r, s, err := api.SignL1Action(action, timestamp)
	if err != nil {
		api.debug("Error signing L1 action: %s", err)
		return nil, err
	}
	request := ExchangeRequest{
		Action:       action,
		Nonce:        timestamp,
		Signature:    ToTypedSig(r, s, v),
		VaultAddress: nil,
	}
	return MakeUniversalRequest[CancelOrderResponse](api, request)
}

// Cancel exact order by OID
func (api *ExchangeAPI) CancelOrderByOID(coin string, orderID int64) (*CancelOrderResponse, error) {
	return api.BulkCancelOrders([]CancelOidWire{{Asset: api.meta[coin].AssetId, Oid: int(orderID)}})
}

// Cancel all orders for a given coin
func (api *ExchangeAPI) CancelAllOrdersByCoin(coin string) (*CancelOrderResponse, error) {
	orders, err := api.infoAPI.GetOpenOrders(api.AccountAddress())
	if err != nil {
		api.debug("Error getting orders: %s", err)
		return nil, err
	}
	var cancels []CancelOidWire
	for _, order := range *orders {
		if coin != order.Coin {
			continue
		}
		cancels = append(cancels, CancelOidWire{Asset: api.meta[coin].AssetId, Oid: int(order.Oid)})
	}
	return api.BulkCancelOrders(cancels)
}

// Cancel all open orders
func (api *ExchangeAPI) CancelAllOrders() (*CancelOrderResponse, error) {
	orders, err := api.infoAPI.GetOpenOrders(api.AccountAddress())
	if err != nil {
		api.debug("Error getting orders: %s", err)
		return nil, err
	}
	if len(*orders) == 0 {
		return nil, APIError{Message: "No open orders to cancel"}
	}
	var cancels []CancelOidWire
	for _, order := range *orders {
		cancels = append(cancels, CancelOidWire{Asset: api.meta[order.Coin].AssetId, Oid: int(order.Oid)})
	}
	return api.BulkCancelOrders(cancels)
}

// Update leverage for a coin
// https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint#update-leverage
func (api *ExchangeAPI) UpdateLeverage(coin string, isCross bool, leverage int) (*DefaultExchangeResponse, error) {
	timestamp := GetNonce()
	action := UpdateLeverageAction{
		Type:     "updateLeverage",
		Asset:    api.meta[coin].AssetId,
		IsCross:  isCross,
		Leverage: leverage,
	}
	v, r, s, err := api.SignL1Action(action, timestamp)
	if err != nil {
		api.debug("Error signing L1 action: %s", err)
		return nil, err
	}
	request := ExchangeRequest{
		Action:       action,
		Nonce:        timestamp,
		Signature:    ToTypedSig(r, s, v),
		VaultAddress: nil,
	}
	return MakeUniversalRequest[DefaultExchangeResponse](api, request)
}

// Initiate a withdraw request
// https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint#initiate-a-withdrawal-request
func (api *ExchangeAPI) Withdraw(destination string, amount float64) (*WithdrawResponse, error) {
	nonce := GetNonce()
	action := WithdrawAction{
		Type:        "withdraw3",
		Destination: destination,
		Amount:      FloatToWire(amount, &SZ_DECIMALS),
		Time:        nonce,
	}
	signatureChainID, chainType := api.getChainParams()
	action.HyperliquidChain = chainType
	action.SignatureChainID = signatureChainID
	v, r, s, err := api.SignWithdrawAction(action)
	if err != nil {
		api.debug("Error signing withdraw action: %s", err)
		return nil, err
	}
	request := &ExchangeRequest{
		Action:       action,
		Nonce:        nonce,
		Signature:    ToTypedSig(r, s, v),
		VaultAddress: nil,
	}
	return MakeUniversalRequest[WithdrawResponse](api, request)
}

// Helper function to get the chain params based on the network type.
func (api *ExchangeAPI) getChainParams() (string, string) {
	if api.IsMainnet() {
		return "0xa4b1", "Mainnet"
	}
	return "0x66eee", "Testnet"
}

// Build bulk orders EIP712 message
func (api *ExchangeAPI) BuildBulkOrdersEIP712(requests []OrderRequest, grouping Grouping) (apitypes.TypedData, error) {
	var wires []OrderWire
	for _, req := range requests {
		wires = append(wires, OrderRequestToWire(req, api.meta))
	}
	timestamp := GetNonce()
	action := OrderWiresToOrderAction(wires, grouping)
	srequest, err := api.BuildEIP712Message(action, timestamp)
	if err != nil {
		api.debug("Error building EIP712 message: %s", err)
		return apitypes.TypedData{}, err
	}
	return SignRequestToEIP712TypedData(srequest), nil
}

// Build order EIP712 message
func (api *ExchangeAPI) BuildOrderEIP712(request OrderRequest, grouping Grouping) (apitypes.TypedData, error) {
	return api.BuildBulkOrdersEIP712([]OrderRequest{request}, grouping)
}

// Spot vs Perp transfer - Not working now
func (api *ExchangeAPI) TransferUSDCSpotVsPerp(amount float64, toPerp bool) (*TransferUSDCSpotPerpResponse, error) {
	nonce := GetNonce()
	action := TransferUSDCSpotPerpAction{
		Type: "spotUser",
		ClassTransfer: ClassTransferData{
			Usdc:   amount,
			ToPerp: toPerp,
		},
	}
	v, r, s, err := api.SignL1Action(action, nonce)
	if err != nil {
		api.debug("Error signing transfer action: %s", err)
		return nil, err
	}
	request := &ExchangeRequest{
		Action:       action,
		Nonce:        nonce,
		Signature:    ToTypedSig(r, s, v),
		VaultAddress: nil,
	}
	return MakeUniversalRequest[TransferUSDCSpotPerpResponse](api, request)
}

// Transfer USDC from spot to perp
func (api *ExchangeAPI) Transfer(amount float64, toPerp bool) (*TransferUSDCSpotPerpResponse, error) {
	nonce := GetNonce()
	action := TransferAction{
		Type:   "usdClassTransfer",
		Amount: fmt.Sprintf("%v", amount),
		ToPerp: toPerp,
		Nonce:  nonce,
	}
	signatureChainID, chainType := api.getChainParams()
	action.HyperliquidChain = chainType
	action.SignatureChainID = signatureChainID
	v, r, s, err := api.SignTransferAction(action)
	if err != nil {
		api.debug("Error signing transfer action: %s", err)
		return nil, err
	}
	request := &ExchangeRequest{
		Action:       action,
		Nonce:        nonce,
		Signature:    ToTypedSig(r, s, v),
		VaultAddress: nil,
	}
	return MakeUniversalRequest[TransferUSDCSpotPerpResponse](api, request)
}
