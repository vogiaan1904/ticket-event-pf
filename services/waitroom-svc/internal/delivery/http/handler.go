package http

// type HTTPHandler struct {
// 	waitroomService service.WaitroomService
// 	logger          logger.Logger
// 	validator       *validator.Validate
// }

// func NewHTTPHandler(waitroomService service.WaitroomService, logger logger.Logger) *HTTPHandler {
// 	return &HTTPHandler{
// 		waitroomService: waitroomService,
// 		logger:          logger,
// 		validator:       validator.New(),
// 	}
// }

// // HealthCheck handles health check requests
// func (h *HTTPHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
// 	response := map[string]interface{}{
// 		"status":  "healthy",
// 		"service": "waitroom-service",
// 		"version": "1.0.0",
// 	}
// 	h.respondJSON(w, http.StatusOK, response)
// }

// // JoinQueue handles join queue requests
// func (h *HTTPHandler) JoinQueue(w http.ResponseWriter, r *http.Request) {
// 	var req models.JoinQueueRequest
// 	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
// 		h.respondError(w, http.StatusBadRequest, "Invalid request body", err)
// 		return
// 	}

// 	// Validate request
// 	if err := h.validator.Struct(req); err != nil {
// 		h.respondError(w, http.StatusBadRequest, "Validation failed", err)
// 		return
// 	}

// 	// Extract metadata from request
// 	req.UserAgent = r.UserAgent()
// 	req.IPAddress = r.RemoteAddr

// 	// Join queue
// 	response, err := h.waitroomService.JoinQueue(r.Context(), &req)
// 	if err != nil {
// 		switch err {
// 		case errors.ErrSessionAlreadyExists:
// 			h.respondError(w, http.StatusConflict, "You already have an active session for this event", err)
// 		case errors.ErrQueueFull:
// 			h.respondError(w, http.StatusServiceUnavailable, "Queue is full", err)
// 		case errors.ErrQueueNotEnabled:
// 			h.respondError(w, http.StatusForbidden, "Queue is not enabled for this event", err)
// 		default:
// 			h.logger.Error("Failed to join queue", "error", err)
// 			h.respondError(w, http.StatusInternalServerError, "Failed to join queue", err)
// 		}
// 		return
// 	}

// 	h.respondJSON(w, http.StatusCreated, response)
// }

// // GetQueueStatus handles get queue status requests
// func (h *HTTPHandler) GetQueueStatus(w http.ResponseWriter, r *http.Request) {
// 	sessionID := chi.URLParam(r, "sessionId")
// 	if sessionID == "" {
// 		h.respondError(w, http.StatusBadRequest, "Session ID is required", nil)
// 		return
// 	}

// 	response, err := h.waitroomService.GetQueueStatus(r.Context(), sessionID)
// 	if err != nil {
// 		switch err {
// 		case errors.ErrSessionNotFound:
// 			h.respondError(w, http.StatusNotFound, "Session not found", err)
// 		case errors.ErrSessionExpired:
// 			h.respondError(w, http.StatusGone, "Session has expired", err)
// 		case errors.ErrInvalidSessionStatus:
// 			h.respondError(w, http.StatusBadRequest, "Invalid session status", err)
// 		default:
// 			h.logger.Error("Failed to get queue status", "error", err, "session_id", sessionID)
// 			h.respondError(w, http.StatusInternalServerError, "Failed to get queue status", err)
// 		}
// 		return
// 	}

// 	h.respondJSON(w, http.StatusOK, response)
// }

// // LeaveQueue handles leave queue requests
// func (h *HTTPHandler) LeaveQueue(w http.ResponseWriter, r *http.Request) {
// 	sessionID := chi.URLParam(r, "sessionId")
// 	if sessionID == "" {
// 		h.respondError(w, http.StatusBadRequest, "Session ID is required", nil)
// 		return
// 	}

// 	if err := h.waitroomService.LeaveQueue(r.Context(), sessionID); err != nil {
// 		switch err {
// 		case errors.ErrSessionNotFound:
// 			h.respondError(w, http.StatusNotFound, "Session not found", err)
// 		default:
// 			h.logger.Error("Failed to leave queue", "error", err, "session_id", sessionID)
// 			h.respondError(w, http.StatusInternalServerError, "Failed to leave queue", err)
// 		}
// 		return
// 	}

// 	response := models.LeaveQueueResponse{
// 		SessionID: sessionID,
// 		Message:   "Successfully left the queue",
// 	}

// 	h.respondJSON(w, http.StatusOK, response)
// }

// // Helper functions

// func (h *HTTPHandler) respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
// 	w.Header().Set("Content-Type", "application/json")
// 	w.WriteHeader(statusCode)
// 	if err := json.NewEncoder(w).Encode(data); err != nil {
// 		h.logger.Error("Failed to encode JSON response", "error", err)
// 	}
// }

// func (h *HTTPHandler) respondError(w http.ResponseWriter, statusCode int, message string, err error) {
// 	response := map[string]interface{}{
// 		"error": message,
// 		"code":  statusCode,
// 	}

// 	if err != nil {
// 		h.logger.Debug("Error response", "message", message, "error", err.Error())
// 	}

// 	h.respondJSON(w, statusCode, response)
// }
