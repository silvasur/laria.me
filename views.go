package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"math"
	"path"
	"sort"
	"strings"
	"time"

	"code.laria.me/laria.me/menu"
)

type ViewMenuItem struct {
	Active bool
	Url    string
	Title  string
}

type ViewMenu [][]ViewMenuItem

func buildViewMenuLevel(
	item *menu.MenuItem,
	current string,
	withNextLevel bool,
) (level []ViewMenuItem, nextLevel []ViewMenuItem) {
	level = make([]ViewMenuItem, 0, len(item.Children))

	for _, child := range item.Children {
		isCur := child.Ident == current

		level = append(level, ViewMenuItem{
			Active: isCur,
			Url:    child.Url,
			Title:  child.Title,
		})

		if isCur && withNextLevel {
			nextLevel, _ = buildViewMenuLevel(child, "", false)
		}
	}

	return
}

func buildViewMenuLevels(item *menu.MenuItem, current string, withNextLevel bool) ViewMenu {
	if item == nil {
		return nil
	}

	viewMenu := buildViewMenuLevels(item.Parent, item.Ident, false)

	level, nextLevel := buildViewMenuLevel(item, current, withNextLevel)

	if len(level) > 0 {
		viewMenu = append(viewMenu, level)
	}

	if len(nextLevel) > 0 {
		viewMenu = append(viewMenu, nextLevel)
	}

	return viewMenu
}

func BuildViewMenu(menu *menu.Menu, current string) ViewMenu {
	curMenu := menu.Root()

	curMenuItem := menu.ByIdent(current)
	if curMenuItem != nil {
		curMenu = curMenuItem.Parent
	}

	return buildViewMenuLevels(curMenu, current, true)
}

type RootData struct {
	Menu  ViewMenu
	Title string
	Main  interface{}
}

type ViewArticle struct {
	Published time.Time
	Slug      string
	Title     string
	Content   template.HTML
	ReadMore  bool
	Tags      []string
}

type Views struct {
	archiveDay   *template.Template
	archive      *template.Template
	archiveMonth *template.Template
	archiveYear  *template.Template
	article      *template.Template
	blog         *template.Template
	content      *template.Template
	search       *template.Template
	start        *template.Template
	tag          *template.Template
	tags         *template.Template
}

func monthText(m int) string {
	switch m {
	case 1:
		return "January"
	case 2:
		return "February"
	case 3:
		return "March"
	case 4:
		return "April"
	case 5:
		return "May"
	case 6:
		return "June"
	case 7:
		return "July"
	case 8:
		return "August"
	case 9:
		return "September"
	case 10:
		return "October"
	case 11:
		return "November"
	case 12:
		return "December"
	default:
		return fmt.Sprintf("<unknown month %d>", m)
	}
}

func nth(n int) string {
	switch n {
	case 1:
		return "1st"
	case 2:
		return "2nd"
	case 3:
		return "3rd"
	default:
		return fmt.Sprintf("%dth", n)
	}
}

type paginationArg struct {
	K, V string
}

type paginationTemplateData struct {
	Action string
	Args   []paginationArg
	Cur    int
	Pages  int
}

var paginationTemplate = template.Must(template.New("").Funcs(template.FuncMap{"seq": func(max int) <-chan int {
	ch := make(chan int)
	go func() {
		defer close(ch)
		for i := 1; i <= max; i++ {
			ch <- i
		}
	}()
	return ch
}}).Parse(`<form action="{{.Action}}" method="get" class="pagination">
	{{- range .Args -}}
		<input type="hidden" name="{{.K}}" value="{{.V}}">
	{{- end -}}
	{{- $cur := .Cur -}}
	<label for="pagination-select">Page:</label>
	<select name="page" id="pagination-select">{{- range (seq .Pages) -}}
		<option {{if eq . $cur}}selected{{end}} value="{{.}}">{{.}}</option>
	{{- end -}}</select>
	<button type="submit">Go to</button>
</form>`))

func normalizeDate(y, m, d int) (int, int, int) {
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	y, month, d := t.Date()

	return y, int(month), d
}

func dayText(y, m, d int) string {
	return fmt.Sprintf("%s %s %d", nth(d), monthText(m), y)
}

func LoadViews(templatesDir string) (Views, error) {
	views := Views{}

	root, err := template.New("root.html").ParseFiles(path.Join(templatesDir, "root.html"))

	if err != nil {
		return views, err
	}

	root.Funcs(template.FuncMap{
		"add": func(nums ...int) int {
			sum := 0
			for _, i := range nums {
				sum = sum + i
			}
			return sum
		},
		"concat": func(ss ...string) string {
			sb := new(strings.Builder)
			for _, s := range ss {
				sb.WriteString(s)
			}
			return sb.String()
		},
		"nth":        nth,
		"day_text":   dayText,
		"month_text": monthText,
		"pagination": func(pages, page int, path string, queryArgs ...string) (template.HTML, error) {
			if len(queryArgs)%2 != 0 {
				return "", fmt.Errorf("pagination: need even number of query args")
			}

			args := make([]paginationArg, 0, len(queryArgs)/2)
			for i := 0; i < len(queryArgs); i += 2 {
				args = append(args, paginationArg{K: queryArgs[i], V: queryArgs[i+1]})
			}

			buf := new(bytes.Buffer)

			err := paginationTemplate.Execute(buf, paginationTemplateData{
				Action: path,
				Args:   args,
				Cur:    page,
				Pages:  pages,
			})

			if err != nil {
				return "", err
			}

			return template.HTML(buf.String()), nil
		},
		"archive_link": func(components ...int) (string, error) {
			switch len(components) {
			case 0:
				return "/blog/archive", nil
			case 1:
				y := components[0]
				return fmt.Sprintf("/blog/%d", y), nil
			case 2:
				y := components[0]
				m := components[1]

				y, m, _ = normalizeDate(y, m, 1)

				return fmt.Sprintf("/blog/%d/%d", y, m), nil
			case 3:
				y := components[0]
				m := components[1]
				d := components[2]

				y, m, d = normalizeDate(y, m, d)

				return fmt.Sprintf("/blog/%d/%d/%d", y, m, d), nil
			default:
				return "", fmt.Errorf("archive_link accepts at most 3 arguments")
			}
		},
	})

	for name, t := range map[string]**template.Template{
		"archive-day":   &(views.archiveDay),
		"archive":       &(views.archive),
		"archive-month": &(views.archiveMonth),
		"archive-year":  &(views.archiveYear),
		"article":       &(views.article),
		"blog":          &(views.blog),
		"content":       &(views.content),
		"search":        &(views.search),
		"start":         &(views.start),
		"tag":           &(views.tag),
		"tags":          &(views.tags),
	} {
		templateFile := path.Join(templatesDir, name+".html")

		if *t, err = template.Must(root.Clone()).ParseFiles(templateFile); err != nil {
			return views, fmt.Errorf("Failed loading template %s: %w", name, err)
		}
	}

	return views, nil
}

func (v Views) RenderArchiveDay(
	w io.Writer,
	menu *menu.Menu,
	curMenu string,
	y, m, d int,
	articles []ViewArticle,
) error {
	return v.archiveDay.Execute(w, RootData{BuildViewMenu(menu, curMenu), dayText(y, m, d), struct {
		Year, Month, Day int
		MonthText        string
		Articles         []ViewArticle
	}{
		Year:      y,
		Month:     m,
		Day:       d,
		MonthText: monthText(m),
		Articles:  articles,
	}})
}

type archiveEntryWithCount struct {
	Num   int
	Count int
}

type archiveEntriesWithCount []archiveEntryWithCount

func (a archiveEntriesWithCount) Len() int           { return len(a) }
func (a archiveEntriesWithCount) Less(i, j int) bool { return a[i].Num < a[j].Num }
func (a archiveEntriesWithCount) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

func buildArchiveEntries(keyedCounts map[int]int) archiveEntriesWithCount {
	entries := make(archiveEntriesWithCount, 0, len(keyedCounts))
	for k, v := range keyedCounts {
		entries = append(entries, archiveEntryWithCount{
			Num:   k,
			Count: v,
		})
	}

	sort.Sort(entries)

	return entries
}

func (v Views) RenderArchive(
	w io.Writer,
	menu *menu.Menu,
	curMenu string,
	countByYear map[int]int,
) error {
	return v.archive.Execute(w, RootData{BuildViewMenu(menu, curMenu), "Archive", struct {
		Years archiveEntriesWithCount
	}{Years: buildArchiveEntries(countByYear)}})
}

func (v Views) RenderArchiveMonth(
	w io.Writer,
	menu *menu.Menu,
	curMenu string,
	year, month int,
	countByDay map[int]int,
) error {
	title := fmt.Sprintf("%s %d", monthText(month), year)

	return v.archiveMonth.Execute(w, RootData{BuildViewMenu(menu, curMenu), title, struct {
		Year, Month int
		Days        archiveEntriesWithCount
	}{
		Year:  year,
		Month: month,
		Days:  buildArchiveEntries(countByDay),
	}})
}

func (v Views) RenderArchiveYear(
	w io.Writer,
	menu *menu.Menu,
	curMenu string,
	year int,
	countByMonth map[int]int,
) error {
	return v.archiveYear.Execute(w, RootData{BuildViewMenu(menu, curMenu), string(year), struct {
		Year   int
		Months archiveEntriesWithCount
	}{
		Year:   year,
		Months: buildArchiveEntries(countByMonth),
	}})
}

func (v Views) RenderArticle(
	w io.Writer,
	menu *menu.Menu,
	curMenu string,
	article ViewArticle,
) error {
	return v.article.Execute(w, RootData{BuildViewMenu(menu, curMenu), article.Title, article})
}

func (v Views) RenderContent(
	w io.Writer,
	menu *menu.Menu,
	curMenu string,
	html template.HTML,
) error {
	return v.content.Execute(w, RootData{BuildViewMenu(menu, curMenu), "", html})
}

func (v Views) RenderSearch(
	w io.Writer,
	menu *menu.Menu,
	curMenu string,
	query string,
	total int,
	results []ViewArticle,
	pages int,
	page int,
) error {
	return v.search.Execute(w, RootData{BuildViewMenu(menu, curMenu), "Search", struct {
		Q           string
		Total       int
		Results     []ViewArticle
		Pages, Page int
	}{
		Q:       query,
		Total:   total,
		Results: results,
		Pages:   pages,
		Page:    page,
	}})
}

func (v Views) RenderStart(
	w io.Writer,
	menu *menu.Menu,
	curMenu string,
	content template.HTML,
	blogArticles []ViewArticle,
) error {
	return v.start.Execute(w, RootData{BuildViewMenu(menu, curMenu), "", struct {
		Content template.HTML
		Blog    []ViewArticle
	}{
		Content: content,
		Blog:    blogArticles,
	}})
}

func (v Views) RenderTag(
	w io.Writer,
	menu *menu.Menu,
	curMenu string,
	tag string,
	articles []ViewArticle,
	pages, page int,
) error {
	return v.tag.Execute(w, RootData{BuildViewMenu(menu, curMenu), "Tag " + tag, struct {
		Tag         string
		Articles    []ViewArticle
		Pages, Page int
	}{
		Tag:      tag,
		Articles: articles,
		Pages:    pages,
		Page:     page,
	}})
}

func (v Views) RenderBlog(
	w io.Writer,
	menu *menu.Menu,
	curMenu string,
	articles []ViewArticle,
	pages, page int,
) error {
	return v.blog.Execute(w, RootData{BuildViewMenu(menu, curMenu), "Blog", struct {
		Articles    []ViewArticle
		Pages, Page int
	}{
		Articles: articles,
		Pages:    pages,
		Page:     page,
	}})
}

type tagcloudTag struct {
	Tag       string
	SizeClass int
}

type tagcloudTags []tagcloudTag

func (t tagcloudTags) Len() int { return len(t) }
func (t tagcloudTags) Less(i, j int) bool {
	return strings.ToLower(t[i].Tag) < strings.ToLower(t[j].Tag)
}
func (t tagcloudTags) Swap(i, j int) { t[i], t[j] = t[j], t[i] }

const tagcloudCategories = 5

func (v Views) RenderTags(w io.Writer, menu *menu.Menu, curMenu string, tagCounts map[string]int) error {
	tags := make(tagcloudTags, 0, len(tagCounts))

	maxCount := 0
	for tag, count := range tagCounts {
		tags = append(tags, tagcloudTag{
			Tag:       tag,
			SizeClass: count,
		})

		if count > maxCount {
			maxCount = count
		}
	}

	for i, tag := range tags {
		tags[i].SizeClass = int(math.Ceil((float64(tag.SizeClass) / float64(maxCount)) * tagcloudCategories))
	}

	sort.Sort(tags)

	return v.tags.Execute(w, RootData{BuildViewMenu(menu, curMenu), "Tags", struct {
		Tags tagcloudTags
	}{
		Tags: tags,
	}})
}
