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
//
// A field the destination does not declare is an error: it is usually a
// misspelled one, and accepting it silently turns an edit the caller believes
// they made into a no-op that answers 200.
func DecodeJSON(w http.ResponseWriter, r *http.Request, destination any) *apperrors.AppError {
	return decodeJSON(w, r, destination, true)
}

// DecodeJSONLenient decodes one JSON request body into destination, ignoring
// fields the destination does not declare. Every other guarantee of DecodeJSON
// holds: the body is still bounded, still required, and still has to be exactly
// one JSON value.
//
// Reserved for the endpoints whose GET and PUT share a path — /me,
// /users/{userKey} and /posts/{postID}. Their responses carry server-owned
// fields the update deliberately does not accept (identifiers, counts,
// timestamps, resolved media URLs), between ten and fourteen of them each, so a
// client that reads the resource, changes one field and sends the whole object
// back — which was correct until unknown fields started being rejected — would
// otherwise get a 400 naming a field it did not add. Declaring those fields on
// the request types instead, the way the feed settings update does, would leave
// PUT /users/{userKey} with fourteen ignored fields around the one it accepts.
//
// Do not reach for this on a new endpoint. It exists for request types that
// predate strict decoding and are paired with a much wider response; a new
// endpoint can simply not be shaped that way.
func DecodeJSONLenient(w http.ResponseWriter, r *http.Request, destination any) *apperrors.AppError {
	return decodeJSON(w, r, destination, false)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any, rejectUnknownFields bool) *apperrors.AppError {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)

	decoder := json.NewDecoder(r.Body)
	if rejectUnknownFields {
		decoder.DisallowUnknownFields()
	}

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
