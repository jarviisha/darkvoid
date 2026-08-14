package httputil

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	apperrors "github.com/jarviisha/darkvoid/pkg/errors"
)

const maxJSONBodyBytes int64 = 1 << 20

// DecodeJSON decodes one strictly validated JSON request body into destination.
func DecodeJSON(w http.ResponseWriter, r *http.Request, destination any) *apperrors.AppError {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return normalizeJSONDecodeError(err)
	}

	var extra any
	if err := decoder.Decode(&extra); !stderrors.Is(err, io.EOF) {
		if err != nil {
			return normalizeJSONDecodeError(err)
		}
		return invalidJSONBody("request body must contain a single JSON value")
	}

	return nil
}

func normalizeJSONDecodeError(err error) *apperrors.AppError {
	var maxBytesError *http.MaxBytesError
	if stderrors.As(err, &maxBytesError) {
		return invalidJSONBody(fmt.Sprintf("request body must not exceed %d bytes", maxBytesError.Limit))
	}

	if stderrors.Is(err, io.EOF) {
		return invalidJSONBody("request body must not be empty")
	}

	var syntaxError *json.SyntaxError
	if stderrors.As(err, &syntaxError) || stderrors.Is(err, io.ErrUnexpectedEOF) {
		return invalidJSONBody("request body contains malformed JSON")
	}

	var typeError *json.UnmarshalTypeError
	if stderrors.As(err, &typeError) {
		if typeError.Field == "" {
			return invalidJSONBody("request body contains a value with an invalid type")
		}
		return invalidJSONBody(fmt.Sprintf("request body contains an invalid value for field %q", typeError.Field))
	}

	if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		return invalidJSONBody("request body contains unknown field " + field)
	}

	var invalidTargetError *json.InvalidUnmarshalError
	if stderrors.As(err, &invalidTargetError) {
		panic(err)
	}

	return invalidJSONBody("request body could not be decoded")
}

func invalidJSONBody(reason string) *apperrors.AppError {
	return apperrors.NewBadRequestError("invalid request body").WithDetail("reason", reason)
}
