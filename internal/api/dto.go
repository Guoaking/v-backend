package api

// This file used to contain redundant DTOs for Swagger.
// We now use Swagger's schema overriding syntax in handler comments:
// @Success 200 {object} response.SuccessResponse{data=service.TargetResponse}
// This reduces code duplication and ensures Swagger docs stay in sync with service models.
