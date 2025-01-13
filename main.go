package main

import (
	"context"
	"database/sql"
	"log"
	"net"
	"strings"
	"time"

	_ "github.com/lib/pq"
	pb "gobook/gobook/proto"

	"github.com/golang-jwt/jwt/v4"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	grpcstatus "google.golang.org/grpc/status"
)

type server struct {
	pb.UnimplementedBookServiceServer
	db *sql.DB
}

var jwtSecret = []byte("your_secret_key")

// Middleware for JWT authentication
func authenticate(ctx context.Context) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Errorf(codes.Unauthenticated, "missing metadata")
	}
	authHeaders, ok := md["authorization"]
	if !ok || len(authHeaders) == 0 {
		return status.Errorf(codes.Unauthenticated, "missing authorization header")
	}
	tokenString := strings.TrimPrefix(authHeaders[0], "Bearer ")
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, status.Errorf(codes.Unauthenticated, "unexpected signing method")
		}
		return jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return status.Errorf(codes.Unauthenticated, "invalid token")
	}

	return nil
}

// Function to generate JWT
func generateJWT(username string) (string, error) {
	claims := jwt.MapClaims{
		"username": username,
		"exp":      time.Now().Add(time.Hour * 1).Unix(), // Token valid for 1 hour
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// Implement RegisterUser
func (s *server) RegisterUser(ctx context.Context, req *pb.RegisterUserRequest) (*pb.RegisterUserResponse, error) {
	_, err := s.db.Exec("INSERT INTO users (id, username, password) VALUES ($1, $2, $3)", req.User.Id, req.User.Username, req.User.Password)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to register user: %v", err)
	}
	return &pb.RegisterUserResponse{Message: "User registered successfully"}, nil
}

// Implement LoginUser
func (s *server) LoginUser(ctx context.Context, req *pb.LoginUserRequest) (*pb.LoginUserResponse, error) {
	var storedPassword string
	err := s.db.QueryRow("SELECT password FROM users WHERE username = $1", req.Username).Scan(&storedPassword)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, status.Errorf(codes.Unauthenticated, "user not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to query user: %v", err)
	}

	if storedPassword != req.Password {
		return nil, status.Errorf(codes.Unauthenticated, "invalid password")
	}

	token, err := generateJWT(req.Username)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate token: %v", err)
	}

	return &pb.LoginUserResponse{Token: token}, nil
}

// Implement AddBook
func (s *server) AddBook(ctx context.Context, req *pb.AddBookRequest) (*pb.AddBookResponse, error) {
	if err := authenticate(ctx); err != nil {
		return nil, err
	}

	log.Printf("Received AddBook request: %+v", req.Book)
	_, err := s.db.Exec("INSERT INTO books (id, title, author, published_date, status) VALUES ($1, $2, $3, $4, $5)",
		req.Book.Id, req.Book.Title, req.Book.Author, req.Book.PublishedDate, req.Book.Status)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to add book: %v", err)
	}

	return &pb.AddBookResponse{Message: "Book added successfully"}, nil
}

// Implement BorrowBook
func (s *server) BorrowBook(ctx context.Context, req *pb.BorrowBookRequest) (*pb.BorrowBookResponse, error) {
	if err := authenticate(ctx); err != nil {
		return nil, err
	}

	// Check if the book is available
	var status string
	err := s.db.QueryRow("SELECT status FROM books WHERE id = $1", req.BookId).Scan(&status)
	log.Printf("Checking book status for Book ID: %s", req.BookId)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, grpcstatus.Errorf(codes.NotFound, "book not found")
		}
		return nil, grpcstatus.Errorf(codes.Internal, "failed to add book: %v", err)
	}

	if status != "Available" {
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "book is not available")
	}

	// Update book status to Borrowed
	_, err = s.db.Exec("UPDATE books SET status = $1 WHERE id = $2", "Borrowed", req.BookId)
	if err != nil {
		return nil, grpcstatus.Errorf(codes.Internal, "failed to update book status: %v", err)
	}

	// Insert into BorrowedBooks table
	_, err = s.db.Exec("INSERT INTO borrowedbooks (id, book_id, user_id, borrowed_date) VALUES ($1, $2, $3, $4)",
		req.BorrowId, req.BookId, req.UserId, time.Now())
	if err != nil {
		return nil, grpcstatus.Errorf(codes.Internal, "failed to record borrowing: %v", err)
	}

	return &pb.BorrowBookResponse{Message: "Book borrowed successfully"}, nil
}

// Implement ReturnBook
func (s *server) ReturnBook(ctx context.Context, req *pb.ReturnBookRequest) (*pb.ReturnBookResponse, error) {
	if err := authenticate(ctx); err != nil {
		return nil, err
	}

	// Check if the borrowing record exists
	var status string
	err := s.db.QueryRow("SELECT status FROM books WHERE id = $1", req.BookId).Scan(&status)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, grpcstatus.Errorf(codes.NotFound, "book not found")
		}
		return nil, grpcstatus.Errorf(codes.Internal, "failed to query book status: %v", err)
	}

	// Check if the book is currently borrowed
	if status != "Borrowed" {
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "book is not borrowed")
	}

	// Update the book status to Available
	_, err = s.db.Exec("UPDATE books SET status = $1 WHERE id = $2", "Available", req.BookId)
	if err != nil {
		return nil, grpcstatus.Errorf(codes.Internal, "failed to update book status: %v", err)
	}

	// Update the borrowed record with the return date
	_, err = s.db.Exec("UPDATE borrowedbooks SET return_date = $1 WHERE id = $2 AND user_id = $3",
		time.Now(), req.BorrowId, req.UserId)
	if err != nil {
		return nil, grpcstatus.Errorf(codes.Internal, "failed to update borrowed record: %v", err)
	}

	return &pb.ReturnBookResponse{Message: "Book returned successfully"}, nil
}

// Implement DeleteBook
func (s *server) DeleteBook(ctx context.Context, req *pb.DeleteBookRequest) (*pb.DeleteBookResponse, error) {
	if err := authenticate(ctx); err != nil {
		return nil, err
	}

	log.Printf("Received DeleteBook request for ID: %s", req.Id)
	_, err := s.db.Exec("DELETE FROM books WHERE id = $1", req.Id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete book: %v", err)
	}

	return &pb.DeleteBookResponse{Message: "Book deleted successfully"}, nil
}

// Implement UpdateBook
func (s *server) UpdateBook(ctx context.Context, req *pb.UpdateBookRequest) (*pb.UpdateBookResponse, error) {
	if err := authenticate(ctx); err != nil {
		return nil, err
	}

	log.Printf("Received UpdateBook request: %+v", req.Book)
	_, err := s.db.Exec("UPDATE books SET title = $1, author = $2, published_date = $3, status = $4 WHERE id = $5",
		req.Book.Title, req.Book.Author, req.Book.PublishedDate, req.Book.Status, req.Book.Id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update book: %v", err)
	}

	return &pb.UpdateBookResponse{Message: "Book updated successfully"}, nil
}

// Implement ListBooks
func (s *server) ListBooks(ctx context.Context, req *pb.ListBooksRequest) (*pb.ListBooksResponse, error) {
	if err := authenticate(ctx); err != nil {
		return nil, err
	}

	log.Printf("Received ListBooks request for User ID: %s", req.UserId)
	rows, err := s.db.Query("SELECT id, title, author, published_date, status FROM books")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to fetch books: %v", err)
	}
	defer rows.Close()

	var books []*pb.Book
	for rows.Next() {
		var book pb.Book
		if err := rows.Scan(&book.Id, &book.Title, &book.Author, &book.PublishedDate, &book.Status); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to parse book: %v", err)
		}
		books = append(books, &book)
	}

	return &pb.ListBooksResponse{Books: books}, nil
}

// Main function to start the server
func main() {
	connStr := "user=postgres password=admin dbname=gradedchalp32 sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to the database: %v", err)
	}
	defer db.Close()

	listener, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterBookServiceServer(grpcServer, &server{db: db})
	log.Println("gRPC server is running on port 50051")

	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
