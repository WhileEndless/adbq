// Package menubar renders native status menus from a platform-independent model.
package menubar

type Item struct {
	Title     string `json:"title"`
	Icon      string `json:"icon,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
	Checked   bool   `json:"checked,omitempty"`
	Separator bool   `json:"separator,omitempty"`
	Children  []Item `json:"children,omitempty"`
	Run       func() `json:"-"`
}
