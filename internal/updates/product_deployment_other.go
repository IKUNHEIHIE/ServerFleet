//go:build !linux

package updates

func NewProductDeployment(ProductDeploymentOptions) (ProductDeployment, error) {
	return nil, ErrArtifactRejected
}
