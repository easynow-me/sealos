package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/labring/sealos/controllers/pkg/types"
	"github.com/labring/sealos/controllers/pkg/utils"
	"github.com/labring/sealos/service/account/dao"
	"github.com/labring/sealos/service/account/helper"
)

// AdminCreateUser creates a new user account
// @Summary Create a new user account
// @Description Create a new user account with initial balance (admin only)
// @Tags Admin
// @Accept json
// @Produce json
// @Param request body helper.CreateUserReq true "Create user request"
// @Success 200 {object} helper.CreateUserResp "successfully created user"
// @Failure 400 {object} helper.ErrorMessage "failed to parse create user request"
// @Failure 401 {object} helper.ErrorMessage "authenticate error"
// @Failure 500 {object} helper.ErrorMessage "failed to create user"
// @Router /admin/v1alpha1/create-user [post]
func AdminCreateUser(c *gin.Context) {
	// Parse the create user request
	req, err := helper.ParseCreateUserReq(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, helper.ErrorMessage{Error: fmt.Sprintf("failed to parse create user request: %v", err)})
		return
	}

	// Authenticate admin request
	if err := authenticateAdminRequest(c); err != nil {
		c.JSON(http.StatusUnauthorized, helper.ErrorMessage{Error: fmt.Sprintf("authenticate error: %v", err)})
		return
	}

	// Check if user already exists
	existingUser, err := dao.DBClient.GetAccount(types.UserQueryOpts{Owner: req.Username})
	if err == nil && existingUser != nil {
		c.JSON(http.StatusBadRequest, helper.ErrorMessage{Error: "user already exists"})
		return
	}

	// Generate user UID
	userUID := uuid.New()

	// Create user in database
	user := &types.User{
		UID:       userUID,
		ID:        req.UserID,
		Name:      req.Username,
		Nickname:  req.Username,
		Status:    types.UserStatusNormal,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// Create user account
	account := &types.Account{
		UserUID:                 userUID,
		Balance:                 req.InitialBalance,
		DeductionBalance:        0,
		EncryptBalance:          "",
		EncryptDeductionBalance: "",
		CreateRegionID:          dao.DBClient.GetLocalRegion().UID.String(),
		CreatedAt:               time.Now().UTC(),
		UpdatedAt:               time.Now().UTC(),
	}

	// Create user workspace
	workspace := &types.Workspace{
		UID:         uuid.New(),
		ID:          req.Username,
		DisplayName: req.Username,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	// Create user workspace relationship
	userWorkspace := &types.UserWorkspace{
		UserCrUID:    userUID,
		WorkspaceUID: workspace.UID,
		Role:         types.RoleOwner,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		IsPrivate:    false,
		Status:       types.JoinStatusInWorkspace,
	}

	// Execute transaction to create user
	if err := dao.DBClient.CreateUser(user, account, workspace, userWorkspace); err != nil {
		c.JSON(http.StatusInternalServerError, helper.ErrorMessage{Error: fmt.Sprintf("failed to create user: %v", err)})
		return
	}

	// Initialize user namespace quota
	if err := initializeUserQuota(req.Username); err != nil {
		// Log error but don't fail the request
		fmt.Printf("failed to initialize user quota: %v\n", err)
	}

	// Return success response
	c.JSON(http.StatusOK, helper.CreateUserResp{
		UserID:    req.UserID,
		Username:  req.Username,
		Balance:   req.InitialBalance,
		CreatedAt: user.CreatedAt,
		Message:   "User created successfully",
	})
}

// initializeUserQuota initializes resource quota for a new user
func initializeUserQuota(username string) error {
	// This is a placeholder for future quota initialization logic
	// You may want to implement actual quota creation logic here
	// This could involve creating Kubernetes ResourceQuota objects
	// or updating your database with quota information
	
	return nil
}

// AdminGetUserToken generates a JWT token for a specified user
// @Summary Get JWT token for a specified user
// @Description Generate a JWT token for a specified user (admin only)
// @Tags Admin
// @Accept json
// @Produce json
// @Param request body helper.GetUserTokenReq true "Get user token request"
// @Success 200 {object} helper.GetUserTokenResp "successfully generated user token"
// @Failure 400 {object} helper.ErrorMessage "failed to parse request"
// @Failure 401 {object} helper.ErrorMessage "authenticate error"
// @Failure 404 {object} helper.ErrorMessage "user not found"
// @Failure 500 {object} helper.ErrorMessage "failed to generate token"
// @Router /admin/v1alpha1/get-user-token [post]
func AdminGetUserToken(c *gin.Context) {
	// Parse the request
	req, err := helper.ParseGetUserTokenReq(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, helper.ErrorMessage{Error: fmt.Sprintf("failed to parse request: %v", err)})
		return
	}

	// Authenticate admin request
	if err := authenticateAdminRequest(c); err != nil {
		c.JSON(http.StatusUnauthorized, helper.ErrorMessage{Error: fmt.Sprintf("authenticate error: %v", err)})
		return
	}

	// Find the user
	var user *types.User
	var userUID uuid.UUID
	
	if req.Username != "" {
		// Query by username
		userQuery := types.UserQueryOpts{Owner: req.Username}
		userInfo, err := dao.DBClient.GetUserID(userQuery)
		if err != nil {
			c.JSON(http.StatusNotFound, helper.ErrorMessage{Error: "user not found"})
			return
		}
		
		// Get full user information
		fullUserQuery := types.UserQueryOpts{ID: userInfo}
		user, err = dao.DBClient.GetUser(&fullUserQuery)
		if err != nil {
			c.JSON(http.StatusNotFound, helper.ErrorMessage{Error: "user not found"})
			return
		}
		userUID = user.UID
	} else if req.UserUID != "" {
		// Parse and use the provided UID
		parsedUID, err := uuid.Parse(req.UserUID)
		if err != nil {
			c.JSON(http.StatusBadRequest, helper.ErrorMessage{Error: "invalid user UID format"})
			return
		}
		userUID = parsedUID
		
		// Get user by UID
		userQuery := types.UserQueryOpts{UID: userUID}
		user, err = dao.DBClient.GetUser(&userQuery)
		if err != nil {
			c.JSON(http.StatusNotFound, helper.ErrorMessage{Error: "user not found"})
			return
		}
	}

	// Get workspace information if workspace ID is provided
	var workspaceUID uuid.UUID
	if req.WorkspaceID != "" {
		workspace, err := dao.DBClient.GetWorkspace(req.WorkspaceID)
		if err != nil {
			c.JSON(http.StatusBadRequest, helper.ErrorMessage{Error: fmt.Sprintf("workspace not found: %v", err)})
			return
		}
		if len(workspace) > 0 {
			workspaceUID = workspace[0].UID
		}
	}

	// Get region information
	regionUID := dao.DBClient.GetLocalRegion().UID.String()

	// Generate JWT token
	jwtUser := utils.JwtUser{
		UserUID:      userUID,
		UserID:       user.ID,
		UserCrName:   user.Name,
		RegionUID:    regionUID,
		WorkspaceID:  req.WorkspaceID,
		WorkspaceUID: workspaceUID.String(),
	}

	token, err := dao.JwtMgr.GenerateToken(jwtUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, helper.ErrorMessage{Error: fmt.Sprintf("failed to generate token: %v", err)})
		return
	}

	// Calculate expiration time (30 minutes from now)
	expiresAt := time.Now().Add(30 * time.Minute)

	// Return success response
	resp := helper.GetUserTokenResp{
		Token: token,
		User: struct {
			UserID       string `json:"userId" bson:"userId" example:"user-123"`
			UserUID      string `json:"userUid" bson:"userUid" example:"550e8400-e29b-41d4-a716-446655440000"`
			Username     string `json:"username" bson:"username" example:"testuser"`
			WorkspaceID  string `json:"workspaceId,omitempty" bson:"workspaceId" example:"workspace-123"`
			WorkspaceUID string `json:"workspaceUid,omitempty" bson:"workspaceUid" example:"660e8400-e29b-41d4-a716-446655440000"`
		}{
			UserID:       user.ID,
			UserUID:      userUID.String(),
			Username:     user.Name,
			WorkspaceID:  req.WorkspaceID,
			WorkspaceUID: workspaceUID.String(),
		},
		ExpiresAt: expiresAt,
		Message:   "Token generated successfully",
	}

	c.JSON(http.StatusOK, resp)
}

