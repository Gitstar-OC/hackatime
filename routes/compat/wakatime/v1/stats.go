package v1

import (
	"github.com/gorilla/mux"
	conf "github.com/muety/wakapi/config"
	"github.com/muety/wakapi/middlewares"
	"github.com/muety/wakapi/models"
	v1 "github.com/muety/wakapi/models/compat/wakatime/v1"
	"github.com/muety/wakapi/services"
	"github.com/muety/wakapi/utils"
	"net/http"
	"time"
)

type StatsHandler struct {
	config      *conf.Config
	userSrvc    services.IUserService
	summarySrvc services.ISummaryService
}

func NewStatsHandler(userService services.IUserService, summaryService services.ISummaryService) *StatsHandler {
	return &StatsHandler{
		userSrvc:    userService,
		summarySrvc: summaryService,
		config:      conf.Get(),
	}
}

func (h *StatsHandler) RegisterRoutes(router *mux.Router) {
	r := router.PathPrefix("/wakatime/v1/users/{user}/stats/{range}").Subrouter()
	r.Use(
		middlewares.NewAuthenticateMiddleware(h.userSrvc).WithOptionalFor([]string{"/"}).Handler,
	)
	r.Methods(http.MethodGet).HandlerFunc(h.Get)
}

// TODO: support filtering (requires https://github.com/muety/wakapi/issues/108)

func (h *StatsHandler) Get(w http.ResponseWriter, r *http.Request) {
	var vars = mux.Vars(r)
	var authorizedUser, requestedUser *models.User

	if u := r.Context().Value(models.UserKey); u != nil {
		authorizedUser = u.(*models.User)
	}

	if authorizedUser != nil && vars["user"] == "current" {
		vars["user"] = authorizedUser.ID
	}

	requestedUser, err := h.userSrvc.GetUserById(vars["user"])
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("user not found"))
		return
	}

	err, rangeFrom, rangeTo := utils.ResolveIntervalRaw(vars["range"])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid range"))
		return
	}

	minStart := utils.StartOfDay(rangeTo.Add(-24 * time.Hour * time.Duration(requestedUser.ShareDataMaxDays)))
	if (authorizedUser == nil || requestedUser.ID != authorizedUser.ID) &&
		(requestedUser.ShareDataMaxDays == 0 || rangeFrom.Before(minStart)) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("requested time range too broad"))
		return
	}

	summary, err, status := h.loadUserSummary(requestedUser, rangeFrom, rangeTo)
	if err != nil {
		w.WriteHeader(status)
		w.Write([]byte(err.Error()))
		return
	}

	stats := v1.NewStatsFrom(summary, &models.Filters{})

	// post filter stats according to user's given sharing permissions
	if !requestedUser.ShareEditors {
		stats.Data.Editors = nil
	}
	if !requestedUser.ShareLanguages {
		stats.Data.Languages = nil
	}
	if !requestedUser.ShareProjects {
		stats.Data.Projects = nil
	}
	if !requestedUser.ShareOSs {
		stats.Data.OperatingSystems = nil
	}
	if !requestedUser.ShareMachines {
		stats.Data.Machines = nil
	}

	utils.RespondJSON(w, http.StatusOK, stats)
}

func (h *StatsHandler) loadUserSummary(user *models.User, start, end time.Time) (*models.Summary, error, int) {
	overallParams := &models.SummaryParams{
		From:      start,
		To:        end,
		User:      user,
		Recompute: false,
	}

	summary, err := h.summarySrvc.Aliased(overallParams.From, overallParams.To, user, h.summarySrvc.Retrieve)
	if err != nil {
		return nil, err, http.StatusInternalServerError
	}

	return summary, nil, http.StatusOK
}
