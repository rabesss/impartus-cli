package app

import (
	"context"
	"errors"

	"github.com/rabesss/impartus-cli/internal/client"
)

// Courses returns the authenticated user's live Impartus catalog.
func (service *Service) Courses(ctx context.Context) (client.Courses, error) {
	if service == nil || service.config == nil || service.catalog == nil {
		return nil, errors.New("application catalog service is not configured")
	}
	return service.catalog.GetCourses(ctx, service.config)
}

// Lectures returns the live lectures for one course.
func (service *Service) Lectures(ctx context.Context, course client.Course) (client.Lectures, error) {
	if service == nil || service.config == nil || service.catalog == nil {
		return nil, errors.New("application catalog service is not configured")
	}
	lectures, err := service.catalog.GetLectures(ctx, service.config, course)
	if err != nil {
		return nil, err
	}
	if service.config.SkipNoAudio {
		lectures = lectures.FilterNoAudio()
	}
	return lectures, nil
}
