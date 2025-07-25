package helper

import (
	"fmt"
	"time"

	"github.com/labring/sealos/controllers/pkg/types"

	"github.com/google/uuid"

	"github.com/labring/sealos/service/account/common"

	"github.com/dustin/go-humanize"

	"github.com/gin-gonic/gin"
)

type AuthReq interface {
	GetAuth() *Auth
	SetAuth(auth *Auth)
}

type NamespaceBillingHistoryReq struct {
	// @Summary Start and end time for the request
	// @Description Start and end time for the request
	// @JSONSchema required
	TimeRange `json:",inline" bson:",inline"`

	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`

	// @Summary Type of the request (optional)
	// @Description Type of the request (optional)
	// @JSONSchema
	Type int `json:"type" bson:"type"`
}

type SetPaymentInvoiceReq struct {
	// @Summary Payment ID list
	// @Description Payment ID list
	// @JSONSchema required
	PaymentIDList []string `json:"paymentIDList" bson:"paymentIDList" binding:"required" example:"[\"payment-id-1\",\"payment-id-2\"]"`

	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`
}

type TransferAmountReq struct {
	// @Summary Transfer amount
	// @Description Transfer amount
	// @JSONSchema required
	Amount int64 `json:"amount" bson:"amount" example:"100000000"`

	// @Summary To user
	// @Description To user
	// @JSONSchema required
	ToUser string `json:"toUser" bson:"toUser" binding:"required" example:"admin"`

	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`

	// @Summary Transfer all
	// @Description Transfer all amount
	TransferAll bool `json:"transferAll" bson:"transferAll"`
}

type ConsumptionRecordReq struct {

	// @Summary Start and end time for the request
	// @Description Start and end time for the request
	// @JSONSchema required
	TimeRange `json:",inline" bson:",inline"`

	// @Summary Namespace
	// @Description Namespace
	Namespace string `json:"namespace,omitempty" bson:"namespace" example:"ns-admin"`

	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`

	// @Summary App type
	// @Description App type
	AppType string `json:"appType,omitempty" bson:"appType" example:"app"`

	// @Summary App Name
	// @Description App Name
	AppName string `json:"appName,omitempty" bson:"appName" example:"app"`
}

type UserTimeRangeReq struct {

	// @Summary Start and end time for the request
	// @Description Start and end time for the request
	// @JSONSchema required
	TimeRange `json:",inline" bson:",inline"`

	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`
}

// ResourceUsage 定义资源使用情况的结构体
type ResourceUsage struct {
	Used       map[uint8]int64 `json:"used"`        // 资源使用量，key为资源类型
	UsedAmount map[uint8]int64 `json:"used_amount"` // 资源使用花费，key为资源类型
	Count      int             `json:"count"`       // 记录数量
}

// AppResourceCostsResponse 定义返回结果的结构体
type AppResourceCostsResponse struct {
	ResourcesByType map[string]*ResourceUsage `json:"resources_by_type,omitempty"`
	AppType         string                    `json:"app_type"`
}

type AppCostsReq struct {
	// @Summary Order ID
	// @Description Order ID
	// @JSONSchema
	OrderID string `json:"orderID,omitempty" bson:"orderID" example:"order-id-1"`

	UserTimeRangeReq `json:",inline" bson:",inline"`

	// @Summary Namespace
	// @Description Namespace
	Namespace string `json:"namespace,omitempty" bson:"namespace" example:"ns-admin"`
	// @Summary App type
	// @Description App type
	AppType string `json:"appType,omitempty" bson:"appType" example:"app"`

	// @Summary App Name
	// @Description App Name
	AppName string `json:"appName,omitempty" bson:"appName" example:"app"`

	// @Summary Page
	// @Description Page
	Page int `json:"page,omitempty" bson:"page" example:"1"`

	// @Summary Page Size
	// @Description Page Size
	PageSize int `json:"pageSize,omitempty" bson:"pageSize" example:"10"`
}

type GetPaymentReq struct {
	// @Summary Payment ID
	// @Description Payment ID
	// @JSONSchema
	PaymentID string `json:"paymentID,omitempty" bson:"paymentID" example:"payment-id-1"`

	// @Summary Invoiced
	// @Description Invoiced
	// @JSONSchema
	Invoiced *bool `json:"invoiced,omitempty" bson:"invoiced" example:"true"`

	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`

	// @Summary Limit request
	// @Description Limit request
	LimitReq `json:",inline" bson:",inline"`
}

type ApplyInvoiceReq struct {
	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`

	// payment id list
	// @Summary Payment ID list
	// @Description Payment ID list
	// @JSONSchema required
	PaymentIDList []string `json:"paymentIDList" bson:"paymentIDList" binding:"required" example:"[\"payment-id-1\",\"payment-id-2\"]"`

	// invoice detail information json
	// @Summary Invoice detail information
	// @Description Invoice detail information
	// @JSONSchema required
	Detail string `json:"detail" bson:"detail" binding:"required" example:"{\"title\":\"title\",\"amount\":100,\"taxRate\":0.06,\"tax\":6,\"total\":106,\"invoiceType\":1,\"invoiceContent\":1,\"invoiceStatus\":1,\"invoiceTime\":\"2021-01-01T00:00:00Z\",\"invoiceNumber\":\"invoice-number-1\",\"invoiceCode\":\"invoice-code-1\",\"invoiceFile\":\"invoice-file-1\"}"`
}

type LimitReq struct {
	// @Summary Page
	// @Description Page
	Page int `json:"page" bson:"page"`

	// @Summary Page Size
	// @Description Page Size
	PageSize int `json:"pageSize" bson:"pageSize"`

	// @Summary Time range
	// @Description Time range
	TimeRange `json:",inline" bson:",inline"`
}

type SetInvoiceStatusReq struct {
	// Invoice id list
	// @Summary Invoice ID list
	// @Description Invoice ID list
	// @JSONSchema required
	InvoiceIDList []string `json:"invoiceIDList" bson:"invoiceIDList" binding:"required" example:"[\"invoice-id-1\",\"invoice-id-2\"]"`

	// Invoice status
	// @Summary Invoice status
	// @Description Invoice status
	// @JSONSchema required
	Status string `json:"status" bson:"status" binding:"required" example:"COMPLETED,REJECTED,PENDING"`

	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`
}

type UseGiftCodeReq struct {
	// @Summary Gift code to be used
	// @Description The code of the gift card to be redeemed
	// @JSONSchema required
	Code string `json:"code" bson:"code" binding:"required" example:"HAPPY2024"`

	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`
}

type GetRealNameInfoReq struct {
	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`
}

type UserUsageReq struct {
	// @Summary Start and end time for the request
	// @Description Start and end time for the request
	// @JSONSchema required
	TimeRange `json:",inline" bson:",inline"`

	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`

	// NamespaceList
	// @Summary Namespace list
	// @Description Namespace list
	// @JSONSchema
	NamespaceList []string `json:"namespaceList" bson:"namespaceList" example:"[\"ns-admin\",\"ns-test1\"]"`
}

type GetInvoiceReq struct {
	// @Summary Invoice ID
	// @Description Invoice ID
	// @JSONSchema
	InvoiceID string `json:"invoiceID,omitempty" bson:"invoiceID" example:"invoice-id-1"`

	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`

	// @Summary Limit request
	// @Description Limit request
	LimitReq `json:",inline" bson:",inline"`
}

type GetCostAppListReq struct {
	// @Summary Authentication information
	// @Description Authentication information
	AuthBase `json:",inline" bson:",inline"`

	// @Summary Namespace
	// @Description Namespace
	Namespace string `json:"namespace" bson:"namespace"`

	// @Summary App type
	// @Description App type
	AppType string `json:"appType" bson:"appType"`

	// @Summary App Name
	// @Description App Name
	AppName string `json:"appName" bson:"appName"`

	// @Summary Limit request
	// @Description Limit request
	LimitReq `json:",inline" bson:",inline"`
}

type AuthBase struct {
	*Auth `json:",inline" bson:",inline"`
}

func (a *AuthBase) GetAuth() *Auth {
	return a.Auth
}

func (a *AuthBase) SetAuth(auth *Auth) {
	a.Auth = auth
}

type RechargeDiscountResp struct {
	DefaultSteps       map[int64]int64 `json:"defaultSteps" bson:"defaultSteps"`
	FirstRechargeSteps map[int64]int64 `json:"firstRechargeDiscount" bson:"firstRechargeDiscount"`
}

type NamespaceBillingHistoryResp struct {
	Data    NamespaceBillingHistoryRespData `json:"data,omitempty" bson:"data,omitempty"`
	Message string                          `json:"message,omitempty" bson:"message" example:"successfully retrieved namespace list"`
}

type NamespaceBillingHistoryRespData struct {
	List []string `json:"list,omitempty" bson:"list,omitempty" example:"[\"ns-admin\",\"ns-test1\"]"`
}

type GetPropertiesResp struct {
	Data    GetPropertiesRespData `json:"data,omitempty" bson:"data,omitempty"`
	Message string                `json:"message,omitempty" bson:"message" example:"successfully retrieved properties"`
}

type GetPropertiesRespData struct {
	Properties []common.PropertyQuery `json:"properties,omitempty" bson:"properties,omitempty"`
}

type ErrorMessage struct {
	Error string `json:"error,omitempty" bson:"error,omitempty" example:"authentication failure"`
}

type TimeRange struct {
	StartTime time.Time `json:"startTime" bson:"startTime" example:"2021-01-01T00:00:00Z"`
	EndTime   time.Time `json:"endTime" bson:"endTime" example:"2021-12-01T00:00:00Z"`
}

type Auth struct {
	Owner      string    `json:"owner" bson:"owner" example:"admin"`
	UserUID    uuid.UUID `json:"userUID" bson:"userUID" example:"user-123"`
	UserID     string    `json:"userID" bson:"userID" example:"admin"`
	KubeConfig string    `json:"kubeConfig" bson:"kubeConfig"`
	Token      string    `json:"token" bson:"token" example:"token"`
}

func ParseNamespaceBillingHistoryReq(c *gin.Context) (*NamespaceBillingHistoryReq, error) {
	nsList := &NamespaceBillingHistoryReq{}
	err := c.ShouldBindJSON(nsList)
	if err != nil {
		return nil, fmt.Errorf("bind json error : %v", err)
	}
	return nsList, nil
}

func ParseSetPaymentInvoiceReq(c *gin.Context) (*SetPaymentInvoiceReq, error) {
	paymentList := &SetPaymentInvoiceReq{}
	err := c.ShouldBindJSON(paymentList)
	if err != nil {
		return nil, fmt.Errorf("bind json error : %v", err)
	}
	return paymentList, nil
}

func ParseTransferAmountReq(c *gin.Context) (*TransferAmountReq, error) {
	transferAmount := &TransferAmountReq{}
	if err := c.ShouldBindJSON(transferAmount); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	if transferAmount.Amount == 0 && !transferAmount.TransferAll {
		return nil, fmt.Errorf("transfer amount cannot be empty")
	}
	return transferAmount, nil
}

func ParseConsumptionRecordReq(c *gin.Context) (*ConsumptionRecordReq, error) {
	consumptionRecord := &ConsumptionRecordReq{}
	if err := c.ShouldBindJSON(consumptionRecord); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	setDefaultTimeRange(&consumptionRecord.TimeRange)
	return consumptionRecord, nil
}

func ParseUserTimeRangeReq(c *gin.Context) (*UserTimeRangeReq, error) {
	userCosts := &UserTimeRangeReq{}
	if err := c.ShouldBindJSON(userCosts); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	setDefaultTimeRange(&userCosts.TimeRange)
	return userCosts, nil
}

func ParsePaymentReq(c *gin.Context) (*GetPaymentReq, error) {
	payment := &GetPaymentReq{}
	if err := c.ShouldBindJSON(payment); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	if payment.Page <= 0 {
		payment.Page = 1
	}
	if payment.PageSize <= 0 {
		payment.PageSize = 10
	}
	return payment, nil
}

func ParseAppCostsReq(c *gin.Context) (*AppCostsReq, error) {
	userCosts := &AppCostsReq{}
	if err := c.ShouldBindJSON(userCosts); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	setDefaultTimeRange(&userCosts.TimeRange)
	return userCosts, nil
}

type GetTransferRecordReq struct {
	UserTimeRangeReq `json:",inline" bson:",inline"`

	// 0: all, 1: in, 2: out
	// @Summary Type of the request
	// @Description Type of the request: 0: all, 1: transfer in, 2: transfer out
	Type int `json:"type,omitempty" bson:"type" example:"0"`

	// @Summary Transfer ID
	// @Description Transfer ID
	TransferID string `json:"transferID,omitempty" bson:"transferID" example:"transfer-id-1"`

	// @Summary Page
	// @Description Page
	Page int `json:"page,omitempty" bson:"page" example:"1"`

	// @Summary Page Size
	// @Description Page Size
	PageSize int `json:"pageSize,omitempty" bson:"pageSize" example:"10"`
}

func ParseGetTransferRecordReq(c *gin.Context) (*GetTransferRecordReq, error) {
	transferReq := &GetTransferRecordReq{}
	if err := c.ShouldBindJSON(transferReq); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	setDefaultTimeRange(&transferReq.TimeRange)
	return transferReq, nil
}

func ParseGetCostAppListReq(c *gin.Context) (*GetCostAppListReq, error) {
	costAppList := &GetCostAppListReq{}
	if err := c.ShouldBindJSON(costAppList); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	setDefaultTimeRange(&costAppList.TimeRange)
	return costAppList, nil
}

func setDefaultTimeRange(timeRange *TimeRange) {
	if timeRange.StartTime.IsZero() {
		timeRange.StartTime = time.Now().Add(-6 * humanize.Month)
	}
	if timeRange.EndTime.IsZero() {
		timeRange.EndTime = time.Now()
	}
}

func ParseCreateUserReq(c *gin.Context) (*CreateUserReq, error) {
	createUserReq := &CreateUserReq{}
	if err := c.ShouldBindJSON(createUserReq); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	// Generate user ID if not provided
	if createUserReq.UserID == "" {
		createUserReq.UserID = fmt.Sprintf("user-%s", uuid.New().String())
	}
	return createUserReq, nil
}

func ParseGetUserTokenReq(c *gin.Context) (*GetUserTokenReq, error) {
	getUserTokenReq := &GetUserTokenReq{}
	if err := c.ShouldBindJSON(getUserTokenReq); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	// Validate that at least username or userUID is provided
	if getUserTokenReq.Username == "" && getUserTokenReq.UserUID == "" {
		return nil, fmt.Errorf("either username or userUID must be provided")
	}
	return getUserTokenReq, nil
}

type CostOverviewResp struct {
	// @Summary Cost overview
	// @Description Cost overview
	Overviews []CostOverview `json:"overviews" bson:"overviews"`

	// @Summary Limit response
	// @Description Limit response
	LimitResp `json:",inline" bson:",inline"`
}

type CostOverview struct {
	// @Summary Amount
	// @Description Amount
	Amount int64 `json:"amount" bson:"amount"`

	// @Summary Namespace
	// @Description Namespace
	Namespace string `json:"namespace" bson:"namespace"`

	// @Summary Region domain
	// @Description Region domain
	RegionDomain string `json:"regionDomain" bson:"regionDomain" example:"region-domain-1"`

	// @Summary App type
	// @Description App type
	AppType uint8  `json:"appType" bson:"appType"`
	AppName string `json:"appName" bson:"appName"`
}

type CreateUserReq struct {
	// @Summary Username
	// @Description Username for the new user
	// @JSONSchema required
	Username string `json:"username" bson:"username" binding:"required" example:"testuser"`

	// @Summary User ID
	// @Description Unique user ID (optional, will be generated if not provided)
	UserID string `json:"userID,omitempty" bson:"userID" example:"user-123"`

	// @Summary Initial balance
	// @Description Initial account balance (optional, default is 0)
	InitialBalance int64 `json:"initialBalance,omitempty" bson:"initialBalance" example:"1000000000"`

	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`
}

type CreateUserResp struct {
	// @Summary User ID
	// @Description The unique ID of the created user
	UserID string `json:"userID" bson:"userID" example:"user-123"`

	// @Summary Username
	// @Description The username of the created user
	Username string `json:"username" bson:"username" example:"testuser"`

	// @Summary Account balance
	// @Description The initial account balance
	Balance int64 `json:"balance" bson:"balance" example:"1000000000"`

	// @Summary Created at
	// @Description The timestamp when the user was created
	CreatedAt time.Time `json:"createdAt" bson:"createdAt"`

	// @Summary Message
	// @Description Response message
	Message string `json:"message" bson:"message" example:"User created successfully"`
}

type GetUserTokenReq struct {
	// @Summary Username or User ID
	// @Description Username or User ID to get token for
	// @JSONSchema required
	Username string `json:"username,omitempty" bson:"username" example:"testuser"`
	
	// @Summary User UID
	// @Description User UID to get token for (optional, username takes precedence)
	UserUID string `json:"userUID,omitempty" bson:"userUID" example:"550e8400-e29b-41d4-a716-446655440000"`

	// @Summary Workspace ID
	// @Description Workspace ID for the token (optional)
	WorkspaceID string `json:"workspaceId,omitempty" bson:"workspaceId" example:"workspace-123"`

	// @Summary Authentication information
	// @Description Authentication information
	// @JSONSchema required
	AuthBase `json:",inline" bson:",inline"`
}

type GetUserTokenResp struct {
	// @Summary JWT Token
	// @Description The JWT token for the specified user
	Token string `json:"token" bson:"token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`

	// @Summary User information
	// @Description Basic user information included in the token
	User struct {
		UserID       string `json:"userId" bson:"userId" example:"user-123"`
		UserUID      string `json:"userUid" bson:"userUid" example:"550e8400-e29b-41d4-a716-446655440000"`
		Username     string `json:"username" bson:"username" example:"testuser"`
		WorkspaceID  string `json:"workspaceId,omitempty" bson:"workspaceId" example:"workspace-123"`
		WorkspaceUID string `json:"workspaceUid,omitempty" bson:"workspaceUid" example:"660e8400-e29b-41d4-a716-446655440000"`
	} `json:"user" bson:"user"`

	// @Summary Expiration
	// @Description Token expiration time
	ExpiresAt time.Time `json:"expiresAt" bson:"expiresAt"`

	// @Summary Message
	// @Description Response message
	Message string `json:"message" bson:"message" example:"Token generated successfully"`
}

type CostAppListResp struct {
	// @Summary Cost app list
	// @Description Cost app list
	Apps []CostApp `json:"apps" bson:"apps"`

	// @Summary Limit response
	// @Description Limit response
	LimitResp `json:",inline" bson:",inline"`
}

type CostApp struct {
	// @Summary Namespace
	// @Description Namespace
	Namespace string `json:"namespace" bson:"namespace"`

	// @Summary App type
	// @Description App type
	AppType uint8 `json:"appType" bson:"appType"`

	// @Summary App Name
	// @Description App Name
	AppName string `json:"appName" bson:"appName"`
}

type LimitResp struct {
	// @Summary Total
	// @Description Total
	Total int64 `json:"total" bson:"total"`

	// @Summary Total page
	// @Description Total page
	TotalPage int64 `json:"totalPage" bson:"totalPage"`
}

func ParseApplyInvoiceReq(c *gin.Context) (*ApplyInvoiceReq, error) {
	applyInvoice := &ApplyInvoiceReq{}
	if err := c.ShouldBindJSON(applyInvoice); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	return applyInvoice, nil
}

func ParseGetInvoiceReq(c *gin.Context) (*GetInvoiceReq, error) {
	invoiceList := &GetInvoiceReq{}
	if err := c.ShouldBindJSON(invoiceList); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	return invoiceList, nil
}

func ParseSetInvoiceStatusReq(c *gin.Context) (*SetInvoiceStatusReq, error) {
	invoiceStatus := &SetInvoiceStatusReq{}
	if err := c.ShouldBindJSON(invoiceStatus); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	return invoiceStatus, nil
}

type UseGiftCodeRespData struct {
	UserID string `json:"userID" bson:"userID" example:"user-123"`
}

type UseGiftCodeResp struct {
	Data    UseGiftCodeRespData `json:"data,omitempty" bson:"data,omitempty"`
	Message string              `json:"message,omitempty" bson:"message" example:"Gift code successfully redeemed"`
}

func ParseUseGiftCodeReq(c *gin.Context) (*UseGiftCodeReq, error) {
	useGiftCode := &UseGiftCodeReq{}
	if err := c.ShouldBindJSON(useGiftCode); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}

	// Additional validation can be added here if needed
	if useGiftCode.Code == "" {
		return nil, fmt.Errorf("gift code cannot be empty")
	}

	return useGiftCode, nil
}

type GetRealNameInfoRespData struct {
	UserID     string `json:"userID" bson:"userID" example:"user-123"`
	IsRealName bool   `json:"isRealName" bson:"isRealName" example:"true"`
}

type GetRealNameInfoResp struct {
	Data    GetRealNameInfoRespData `json:"data,omitempty" bson:"data,omitempty"`
	Message string                  `json:"message,omitempty" bson:"message" example:"Successfully retrieved real name information"`
}

func ParseGetRealNameInfoReq(c *gin.Context) (*GetRealNameInfoReq, error) {
	getRealNameInfoReq := &GetRealNameInfoReq{}
	if err := c.ShouldBindJSON(getRealNameInfoReq); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}

	return getRealNameInfoReq, nil
}

func ParseUserUsageReq(c *gin.Context) (*UserUsageReq, error) {
	userUsage := &UserUsageReq{}
	if err := c.ShouldBindJSON(userUsage); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	if userUsage.StartTime.IsZero() {
		userUsage.StartTime = time.Now().Add(-2 * time.Minute)
	}
	if userUsage.EndTime.IsZero() {
		userUsage.EndTime = time.Now()
	}
	return userUsage, nil
}

type AdminChargeBillingReq struct {
	Amount    int64     `json:"amount" bson:"amount" example:"100000000"`
	Namespace string    `json:"namespace" bson:"namespace" example:"ns-admin"`
	Owner     string    `json:"owner" bson:"owner" example:"admin"`
	AppType   string    `json:"appType" bson:"appType"`
	AppName   string    `json:"appName" bson:"appName"`
	UserUID   uuid.UUID `json:"userUID" bson:"userUID"`
}

func ParseAdminChargeBillingReq(c *gin.Context) (*AdminChargeBillingReq, error) {
	rechargeBilling := &AdminChargeBillingReq{}
	if err := c.ShouldBindJSON(rechargeBilling); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	return rechargeBilling, nil
}

type AdminFlushSubscriptionQuotaReq struct {
	UserUID  uuid.UUID `json:"userUID" bson:"userUID"`
	PlanName string    `json:"planName" bson:"planName"`
	PlanID   uuid.UUID `json:"planID" bson:"planID"`
}

func ParseAdminFlushSubscriptionQuotaReq(c *gin.Context) (*AdminFlushSubscriptionQuotaReq, error) {
	flushSubscriptionQuota := &AdminFlushSubscriptionQuotaReq{}
	if err := c.ShouldBindJSON(flushSubscriptionQuota); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	if flushSubscriptionQuota.UserUID == uuid.Nil {
		return nil, fmt.Errorf("userUID cannot be empty")
	}
	if flushSubscriptionQuota.PlanID == uuid.Nil {
		return nil, fmt.Errorf("planID cannot be empty")
	}
	if flushSubscriptionQuota.PlanName == "" {
		return nil, fmt.Errorf("planName cannot be empty")
	}
	return flushSubscriptionQuota, nil
}

type AdminFlushDebtResourceStatusReq struct {
	UserUID           uuid.UUID            `json:"userUID" bson:"userUID"`
	LastDebtStatus    types.DebtStatusType `json:"lastDebtStatus" bson:"lastDebtStatus"`
	CurrentDebtStatus types.DebtStatusType `json:"currentDebtStatus" bson:"currentDebtStatus"`
	IsBasicUser       bool                 `json:"isBasicUser" bson:"isBasicUser"`
}

func ParseAdminFlushDebtResourceStatusReq(c *gin.Context) (*AdminFlushDebtResourceStatusReq, error) {
	flushDebtResourceStatus := &AdminFlushDebtResourceStatusReq{}
	if err := c.ShouldBindJSON(flushDebtResourceStatus); err != nil {
		return nil, fmt.Errorf("bind json error: %v", err)
	}
	if flushDebtResourceStatus.UserUID == uuid.Nil {
		return nil, fmt.Errorf("userUID cannot be empty")
	}
	if flushDebtResourceStatus.CurrentDebtStatus == "" {
		return nil, fmt.Errorf("currentDebtStatus cannot be empty")
	}
	return flushDebtResourceStatus, nil
}
