package feed

import (
	"html"
	"io"
	"strings"
	"text/template"
)

const feedTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"
     xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"
     xmlns:content="http://purl.org/rss/1.0/modules/content/"
     xmlns:podcast="https://podcastindex.org/namespace/1.0"
     xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>{{x .Title}}</title>
    <link>{{x .Link}}</link>
    <description>{{x .Description}}</description>
    <language>{{x .Language}}</language>
    <lastBuildDate>{{x .BuildDate}}</lastBuildDate>
    <generator>vocalis</generator>
    <itunes:author>{{x .Author}}</itunes:author>
    <itunes:summary>{{x .Description}}</itunes:summary>
    <itunes:explicit>{{x .Explicit}}</itunes:explicit>
    <itunes:type>{{x .Type}}</itunes:type>
{{- if .OwnerEmail}}
    <itunes:owner>
      <itunes:name>{{x .Author}}</itunes:name>
      <itunes:email>{{x .OwnerEmail}}</itunes:email>
    </itunes:owner>
{{- end}}
{{- if .SelfURL}}
    <atom:link href="{{x .SelfURL}}" rel="self" type="application/rss+xml"/>
{{- end}}
{{- if .CoverURL}}
    <itunes:image href="{{x .CoverURL}}"/>
    <image>
      <url>{{x .CoverURL}}</url>
      <title>{{x .Title}}</title>
      <link>{{x .Link}}</link>
    </image>
{{- end}}
{{- range .Category}}
    <itunes:category text="{{x .}}"/>
{{- end}}
{{- range .Items}}
    <item>
      <title>{{x .Title}}</title>
      <itunes:title>{{x .Title}}</itunes:title>
      <guid isPermaLink="false">{{x .GUID}}</guid>
      <pubDate>{{x .PubDate}}</pubDate>
      <enclosure url="{{x .URL}}" length="{{.Length}}" type="{{x .MIME}}"/>
{{- if .Duration}}
      <itunes:duration>{{x .Duration}}</itunes:duration>
{{- end}}
      <itunes:season>{{.Season}}</itunes:season>
      <itunes:episode>{{.Episode}}</itunes:episode>
      <itunes:episodeType>{{x .EpisodeType}}</itunes:episodeType>
{{- if .CoverURL}}
      <itunes:image href="{{x .CoverURL}}"/>
{{- end}}
{{- if .ChaptersURL}}
      <podcast:chapters url="{{x .ChaptersURL}}" type="application/json+chapters"/>
{{- end}}
      <description>{{x .Description}}</description>
{{- if not .Compact}}
      <itunes:summary>{{x .Description}}</itunes:summary>
      <content:encoded><![CDATA[<p>{{cdata .Description}}</p>]]></content:encoded>
{{- end}}
    </item>
{{- end}}
  </channel>
</rss>
`

var tmpl = template.Must(template.New("feed").Funcs(template.FuncMap{
	"x": Escape,
	// cdata escapes a description for the HTML payload of content:encoded and
	// makes sure the CDATA section cannot be terminated early.
	"cdata": func(s string) string {
		return strings.ReplaceAll(html.EscapeString(s), "]]>", "]]]]><![CDATA[>")
	},
}).Parse(feedTemplate))

type templateData struct {
	*Feed
	BuildDate string
}

// Render writes the RSS document for f.
func Render(w io.Writer, f *Feed) error {
	return tmpl.Execute(w, templateData{Feed: f, BuildDate: nowRFC1123Z()})
}
