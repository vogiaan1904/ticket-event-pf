package response

import (
	"net/http"

	pkgErrors "github.com/vogiaan1904/ticketbottle-waitroom/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Resp struct {
	ErrorCode int    `json:"error_code"`
	Message   string `json:"message"`
	Data      any    `json:"data,omitempty"`
	Errors    any    `json:"errors,omitempty"`
}

func parseHttpError(err error) (int, Resp) {
	switch parsedErr := err.(type) {
	case *pkgErrors.HTTPError:
		statusCode := parsedErr.StatusCode
		if statusCode == 0 {
			statusCode = http.StatusBadRequest
		}

		return statusCode, Resp{
			ErrorCode: parsedErr.Code,
			Message:   parsedErr.Message,
		}
	default:
		return http.StatusInternalServerError, Resp{
			ErrorCode: 500,
			Message:   "Internal server error",
		}
	}
}

func ParseGRPCError(err error) error {
	switch parsedErr := err.(type) {
	case *pkgErrors.GRPCError:
		// No fallback for a zero GrpcCode: it used to become InvalidArgument,
		// which silently gave every error the wrong class.
		return status.Error(parsedErr.GrpcCode, parsedErr.Error())
	default:
		return status.Error(codes.Internal, "Internal server error")
	}
}
