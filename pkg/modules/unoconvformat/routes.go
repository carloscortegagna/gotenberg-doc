package unoconvformat

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gotenberg/gotenberg/v7/pkg/modules/api"
	"github.com/labstack/echo/v4"
)

// convertRoute returns an api.Route which can convert documents with LibreOffice
func convertRoute(uno API) api.Route {
	return api.Route{
		Method:      http.MethodPost,
		Path:        "/forms/unoconvformat/convert",
		IsMultipart: true,
		Handler: func(c echo.Context) error {
			ctx := c.Get("context").(*api.Context)

			// Let's get the data from the form and validate them.
			var (
				inputPaths       []string
				nativePageRanges string
				format           string
			)

			err := ctx.FormData().
				MandatoryPaths(uno.Extensions(), &inputPaths).
				String("nativePageRanges", &nativePageRanges, "").
				MandatoryString("format", &format).
				Validate()

			if err != nil {
				return fmt.Errorf("validate form data: %w", err)
			}

			// Alright, let's convert each document.

			outputPaths := make([]string, len(inputPaths))

			for i, inputPath := range inputPaths {
				outputPaths[i] = ctx.GeneratePath("." + format)

				options := Options{
					PageRanges: nativePageRanges,
					Format:     format,
				}

				err = uno.Convert(ctx, ctx.Log(), inputPath, outputPaths[i], options)

				if err != nil {
					if errors.Is(err, ErrMalformedPageRanges) {
						return api.WrapError(
							fmt.Errorf("convert: %w", err),
							api.NewSentinelHTTPError(http.StatusBadRequest, fmt.Sprintf("Malformed page ranges '%s' (nativePageRanges)", options.PageRanges)),
						)
					}

					if errors.Is(err, ErrInvalidFormat) {
						return api.WrapError(
							fmt.Errorf("convert: %w", err),
							api.NewSentinelHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format '%s' (format)", options.Format)),
						)
					}

					return fmt.Errorf("convert: %w", err)
				}
			}

			// Last but not least, add the output paths to the context so that
			// the API is able to send them as a response to the client.

			err = ctx.AddOutputPaths(outputPaths...)
			if err != nil {
				return fmt.Errorf("add output paths: %w", err)
			}

			return nil
		},
	}
}
