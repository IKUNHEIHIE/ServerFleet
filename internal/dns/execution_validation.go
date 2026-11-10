package dns

// ValidateExecutionRecord shares the owner's existing input rules with typed
// unified-task admission. It authorizes nothing and performs no provider I/O.
func ValidateExecutionRecord(input RecordInput) error {
	_, err := validateRecordInput(input)
	return err
}
