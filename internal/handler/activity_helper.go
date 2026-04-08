package handler

import (
	"net"
	"net/http"

	"olt-monitor/internal/domain"
	"olt-monitor/internal/service"
)

func logActivity(activity *service.ActivityService, r *http.Request, action, target string, detail map[string]interface{}) {
	if activity == nil || r == nil {
		return
	}

	user := "system"
	role := ""
	if u, ok := GetUserFromContext(r.Context()); ok {
		user = u.Username
		role = u.Role
	}

	remoteIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remoteIP); err == nil {
		remoteIP = host
	}

	activity.Log(r.Context(), domain.Activity{
		User:     user,
		Role:     role,
		Action:   action,
		Target:   target,
		Detail:   detail,
		RemoteIP: remoteIP,
	})
}
