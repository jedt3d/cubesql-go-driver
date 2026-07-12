package cubesql

import "context"

func contextError(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidArgument
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
