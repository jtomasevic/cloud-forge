package provisioner

// ProvisionRequest is the HTTP request body for POST /vpc/provision.
// It is the REST-layer model: it must never be passed below this layer.
// Validation lives on this type; transformation to the service layer is in
// models_transform.go.
type ProvisionRequest struct {
	TenantID    string `json:"tenant_id"`
	DisplayName string `json:"display_name"`
	Plan        string `json:"plan"`
}
