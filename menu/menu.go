package menu

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type MenuItem struct {
	Title    string
	Ident    string
	Url      string
	Children []*MenuItem
	Parent   *MenuItem
}

func (it MenuItem) IsRoot() bool {
	return it.Parent == nil
}

type Menu struct {
	byIdent map[string]*MenuItem
	root    *MenuItem
}

func (m Menu) Root() *MenuItem {
	return m.root
}

func (m Menu) ByIdent(ident string) *MenuItem {
	return m.byIdent[ident]
}

type JsonMenuItem struct {
	Title    string
	Ident    string
	Url      string
	Children []JsonMenuItem `json:",omitempty"`
}

type JsonMenu []JsonMenuItem

func (m *Menu) addJsonMenuItems(jsonItems []JsonMenuItem, parent *MenuItem) {
	for _, jsonItem := range jsonItems {
		item := MenuItem{
			Title:    jsonItem.Title,
			Ident:    jsonItem.Ident,
			Url:      jsonItem.Url,
			Children: make([]*MenuItem, 0, len(jsonItem.Children)),
			Parent:   parent,
		}

		parent.Children = append(parent.Children, &item)
		m.byIdent[jsonItem.Ident] = &item

		m.addJsonMenuItems(jsonItem.Children, &item)
	}
}

func (jm JsonMenu) toMenu() *Menu {
	root := MenuItem{
		Children: make([]*MenuItem, 0, len(jm)),
	}

	menu := &Menu{
		byIdent: make(map[string]*MenuItem),
		root:    &root,
	}

	menu.addJsonMenuItems(jm, &root)

	return menu
}

func Parse(r io.Reader) (*Menu, error) {
	var jsonMenu JsonMenu
	if err := json.NewDecoder(r).Decode(&jsonMenu); err != nil {
		return nil, err
	}
	return jsonMenu.toMenu(), nil
}

func LoadFromFile(filename string) (*Menu, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("Could not load menu from %s: %v", filename, err)
	}
	defer f.Close()

	return Parse(f)
}
