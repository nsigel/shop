package cli

import (
	"github.com/spf13/cobra"

	"github.com/nsigel/deslop/deslop"
)

func (c *CLI) newProductCmd() *cobra.Command {
	var deslopOn bool

	cmd := &cobra.Command{
		Use:   "product <product-id>",
		Short: "Get full product details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel, s, err := c.resolveStore(cmd)
			if err != nil {
				return err
			}
			defer cancel()

			product, err := s.Product(ctx, args[0])
			if err != nil {
				return err
			}

			if deslopOn {
				if product.Attributes == nil {
					product.Attributes = map[string]any{}
				}
				product.Attributes["deslop"] = deslop.EvaluateProduct(product, deslop.DefaultOptions())
			}

			return c.outputJSON(product)
		},
	}

	cmd.Flags().BoolVar(&deslopOn, "deslop", false, "annotate the product with deslop pseudo-brand verdicts")

	return cmd
}
