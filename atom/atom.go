package atom

import "time"

type Link struct {
	XMLName struct{} `xml:"link"`

	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
}

type Summary struct {
	XMLName struct{} `xml:"summary"`

	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

type Entry struct {
	XMLName struct{} `xml:"entry"`

	Title   string    `xml:"title"`
	Id      string    `xml:"id"`
	Updated time.Time `xml:"updated"`
	Summary Summary
	Links   []Link
}

type Feed struct {
	XMLName struct{} `xml:"http://www.w3.org/2005/Atom feed"`

	Title       string `xml:"title"`
	Links       []Link
	Id          string    `xml:"id"`
	AuthorName  string    `xml:"author>name"`
	AuthorEmail string    `xml:"author>email"`
	AuthorUri   string    `xml:"author>uri"`
	Updated     time.Time `xml:"updated"`
	Entries     []Entry
}
