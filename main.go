package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Notification struct {
	ID string `json:"id"`

	Repository struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`

	Subject struct {
		Title            string `json:"title"`
		URL              string `json:"url"`
		LatestCommentURL string `json:"latest_comment_url"`
		Type             string `json:"type"`
	} `json:"subject"`

	Reason    string `json:"reason"`
	Unread    bool   `json:"unread"`
	UpdatedAt string `json:"updated_at"`
	URL       string `json:"url"`
}

type notificationItem struct {
	notification Notification
}

func (i notificationItem) Title() string {
	prefix := "  "
	if i.notification.Unread {
		prefix = "● "
	}

	return prefix + i.notification.Subject.Title
}

func (i notificationItem) Description() string {
	n := i.notification

	return fmt.Sprintf(
		"%s · %s · %s",
		n.Repository.FullName,
		n.Subject.Type,
		n.Reason,
	)
}

func (i notificationItem) FilterValue() string {
	n := i.notification

	return strings.Join([]string{
		n.Subject.Title,
		n.Repository.FullName,
		n.Subject.Type,
		n.Reason,
	}, " ")
}

type notificationsLoadedMsg struct {
	notifications []Notification
}

type loadErrorMsg struct {
	err error
}

type dismissedMsg struct {
	id string
}

type dismissErrorMsg struct {
	err error
}

type pollMsg struct{}

type model struct {
	list        list.Model
	loading     bool
	err         error
	status      string
	width       int
	height      int
	seen        map[string]struct{}
	initialized bool
}

var (
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))
)

func main() {
	m := newModel()

	p := tea.NewProgram(m)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newModel() model {
	delegate := list.NewDefaultDelegate()

	l := list.New(
		[]list.Item{},
		delegate,
		0,
		0,
	)

	l.Title = "GitHub Notifications"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)

	return model{
		list:    l,
		loading: true,
		seen:    make(map[string]struct{}),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		loadNotifications,
		pollEveryMinute(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		m.list.SetSize(msg.Width, msg.Height-3)

	case pollMsg:
		m.loading = true
		m.status = "checking for notifications..."

		return m, tea.Batch(
			loadNotifications,
			pollEveryMinute(),
		)

	case notificationsLoadedMsg:
		items := make([]list.Item, 0, len(msg.notifications))
		var newNotifications []Notification

		for _, n := range msg.notifications {
			items = append(items, notificationItem{
				notification: n,
			})

			// Don't notify about notifications that already existed
			// when the application first started.
			if m.initialized {
				if _, seen := m.seen[n.ID]; !seen {
					newNotifications = append(newNotifications, n)
				}
			}

			m.seen[n.ID] = struct{}{}
		}

		m.list.SetItems(items)

		m.loading = false
		m.err = nil
		m.initialized = true
		m.status = fmt.Sprintf(
			"%d notifications · next check in 1m",
			len(items),
		)

		if len(newNotifications) > 0 {
			return m, notifyNewNotifications(newNotifications)
		}

	case loadErrorMsg:
		m.loading = false
		m.err = msg.err
		m.status = "failed to load notifications"

	case dismissedMsg:
		m.loading = false
		m.err = nil
		m.status = "notification dismissed"

		items := m.list.Items()

		for i, item := range items {
			n, ok := item.(notificationItem)
			if !ok {
				continue
			}

			if n.notification.ID == msg.id {
				m.list.RemoveItem(i)
				return m, nil
			}
		}

	case dismissErrorMsg:
		m.loading = false
		m.err = msg.err
		m.status = "failed to dismiss notification"

	case tea.KeyPressMsg:
		// Let the list handle keys while filtering.
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "o", "enter":
			item := m.list.SelectedItem()
			if item == nil {
				return m, nil
			}

			n, ok := item.(notificationItem)
			if !ok {
				return m, nil
			}

			return m, openBrowser(n.notification.Subject.URL)

		case "d":
			item := m.list.SelectedItem()
			if item == nil {
				return m, nil
			}

			n, ok := item.(notificationItem)
			if !ok {
				return m, nil
			}

			m.loading = true
			m.status = "dismissing..."

			return m, dismissNotification(n.notification.ID)
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)

	return m, cmd
}

func (m model) View() tea.View {
	var b strings.Builder

	if m.err != nil {
		b.WriteString(errorStyle.Render(
			"Error: " + m.err.Error(),
		))
		b.WriteString("\n\n")
	}

	b.WriteString(m.list.View())

	b.WriteString("\n")

	if m.loading {
		b.WriteString(statusStyle.Render("Checking..."))
	} else {
		b.WriteString(statusStyle.Render(m.status))
	}

	b.WriteString("\n")

	b.WriteString(helpStyle.Render(
		"↑/↓ navigate  enter/o open  d dismiss  / filter  q quit",
	))

	return tea.NewView(b.String())
}

func pollEveryMinute() tea.Cmd {
	return tea.Tick(time.Minute, func(time.Time) tea.Msg {
		return pollMsg{}
	})
}

func loadNotifications() tea.Msg {
	cmd := exec.Command(
		"gh",
		"api",
		"notifications",
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return loadErrorMsg{
				err: fmt.Errorf(
					"gh api notifications: %s",
					strings.TrimSpace(stderr.String()),
				),
			}
		}

		return loadErrorMsg{err: err}
	}

	var notifications []Notification

	if err := json.Unmarshal(stdout.Bytes(), &notifications); err != nil {
		return loadErrorMsg{
			err: fmt.Errorf(
				"parse notifications: %w",
				err,
			),
		}
	}

	return notificationsLoadedMsg{
		notifications: notifications,
	}
}

func dismissNotification(id string) tea.Cmd {
	return func() tea.Msg {
		if id == "" {
			return dismissErrorMsg{
				err: errors.New(
					"notification has no thread ID",
				),
			}
		}

		cmd := exec.Command(
			"gh",
			"api",
			"--method",
			"DELETE",
			"notifications/threads/"+id,
		)

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			if stderr.Len() > 0 {
				return dismissErrorMsg{
					err: fmt.Errorf(
						"gh api delete notification: %s",
						strings.TrimSpace(stderr.String()),
					),
				}
			}

			return dismissErrorMsg{err: err}
		}

		return dismissedMsg{
			id: id,
		}
	}
}

func openBrowser(apiURL string) tea.Cmd {
	return func() tea.Msg {
		url := githubWebURL(apiURL)

		var cmd *exec.Cmd

		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)

		case "windows":
			cmd = exec.Command(
				"cmd",
				"/c",
				"start",
				"",
				url,
			)

		default:
			cmd = exec.Command(
				"xdg-open",
				url,
			)
		}

		_ = cmd.Start()

		return nil
	}
}

func githubWebURL(apiURL string) string {
	if !strings.HasPrefix(
		apiURL,
		"https://api.github.com/",
	) {
		return apiURL
	}

	url := strings.TrimPrefix(
		apiURL,
		"https://api.github.com/",
	)

	url = strings.TrimPrefix(
		url,
		"repos/",
	)

	url = strings.Replace(
		url,
		"/pulls/",
		"/pull/",
		1,
	)

	url = strings.Replace(
		url,
		"/issues/",
		"/issues/",
		1,
	)

	url = strings.Replace(
		url,
		"/commits/",
		"/commit/",
		1,
	)

	return "https://github.com/" + url
}

func notifyNewNotifications(notifications []Notification) tea.Cmd {
	return func() tea.Msg {
		if len(notifications) == 0 {
			return nil
		}

		if runtime.GOOS != "darwin" {
			return nil
		}

		if len(notifications) == 1 {
			notifyMacOS(notifications[0])
			return nil
		}

		notifyMacOSSummary(notifications)

		return nil
	}
}

func notifyMacOS(n Notification) {
	title := escapeAppleScript(
		n.Subject.Title,
	)

	subtitle := escapeAppleScript(
		n.Repository.FullName,
	)

	script := fmt.Sprintf(
		`display notification "%s" with title "GitHub" subtitle "%s" sound name "Glass"`,
		title,
		subtitle,
	)

	_ = exec.Command(
		"osascript",
		"-e",
		script,
	).Run()
}

func notifyMacOSSummary(
	notifications []Notification,
) {
	first := notifications[0]

	title := fmt.Sprintf(
		"%d new GitHub notifications",
		len(notifications),
	)

	body := first.Subject.Title

	if len(notifications) > 1 {
		body += fmt.Sprintf(
			" (+%d more)",
			len(notifications)-1,
		)
	}

	script := fmt.Sprintf(
		`display notification "%s" with title "%s" subtitle "%s" sound name "Glass"`,
		escapeAppleScript(body),
		escapeAppleScript(title),
		escapeAppleScript(first.Repository.FullName),
	)

	_ = exec.Command(
		"osascript",
		"-e",
		script,
	).Run()
}

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
