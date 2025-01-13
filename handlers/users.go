package handlers

import (
	"context"
	"database/sql"

	"gobook/auth"
	pb "gobook/gobook/proto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedBookServiceServer
	DB *sql.DB
}

func (s *Server) RegisterUser(ctx context.Context, req *pb.RegisterUserRequest) (*pb.RegisterUserResponse, error) {
	_, err := s.DB.Exec("INSERT INTO users (id, username, password) VALUES ($1, $2, $3)", req.User.Id, req.User.Username, req.User.Password)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to register user: %v", err)
	}
	return &pb.RegisterUserResponse{Message: "User registered successfully"}, nil
}

func (s *Server) LoginUser(ctx context.Context, req *pb.LoginUserRequest) (*pb.LoginUserResponse, error) {
	var storedPassword string
	err := s.DB.QueryRow("SELECT password FROM users WHERE username = $1", req.Username).Scan(&storedPassword)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, status.Errorf(codes.Unauthenticated, "user not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to query user: %v", err)
	}
	if storedPassword != req.Password {
		return nil, status.Errorf(codes.Unauthenticated, "invalid password")
	}
	token, err := auth.GenerateJWT(req.Username)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate token: %v", err)
	}
	return &pb.LoginUserResponse{Token: token}, nil
}
