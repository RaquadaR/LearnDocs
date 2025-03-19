package main

import (
        "crypto/tls"
        "fmt"
        "log"
        "net/smtp"
        "os"
        "time"

        "github.com/joho/godotenv" // For loading .env variables
)

func main() {
        // Load environment variables from .env file
        err := godotenv.Load()
        if err != nil {
                log.Fatalf("Error loading .env file: %v", err)
        }

        smtpServer := os.Getenv("SMTP_SERVER")
        smtpPort := os.Getenv("SMTP_PORT")
        smtpUsername := os.Getenv("SMTP_USERNAME")
        smtpPassword := os.Getenv("SMTP_PASSWORD")
        recipient := os.Getenv("RECIPIENT_EMAIL")

        if smtpServer == "" || smtpPort == "" || smtpUsername == "" || smtpPassword == "" || recipient == "" {
                log.Fatal("Missing required environment variables")
        }

        // Set up the message
        subject := "Automated Email Alert"
        body := fmt.Sprintf("This is an automated email alert sent at %s.", time.Now().Format(time.RFC3339))
        message := []byte(fmt.Sprintf("To: %s\r\n"+
                "Subject: %s\r\n"+
                "\r\n"+
                "%s\r\n",
                recipient, subject, body))

        // Authentication.
        auth := smtp.PlainAuth("", smtpUsername, smtpPassword, smtpServer)

        // TLS configuration.
        tlsConfig := &tls.Config{
                InsecureSkipVerify: false, // In production, ensure proper certificate verification.
                ServerName:         smtpServer,
        }

        // Connect to the SMTP server using TLS.
        conn, err := tls.Dial("tcp", smtpServer+":"+smtpPort, tlsConfig)
        if err != nil {
                log.Fatalf("Error connecting to SMTP server: %v", err)
        }

        // Create an SMTP client.
        client, err := smtp.NewClient(conn, smtpServer)
        if err != nil {
                log.Fatalf("Error creating SMTP client: %v", err)
        }

        // Authenticate with the SMTP server.
        if err := client.Auth(auth); err != nil {
                log.Fatalf("Error authenticating: %v", err)
        }

        // Set the sender and recipient.
        if err := client.Mail(smtpUsername); err != nil {
                log.Fatalf("Error setting sender: %v", err)
        }
        if err := client.Rcpt(recipient); err != nil {
                log.Fatalf("Error setting recipient: %v", err)
        }

        // Send the email body.
        writer, err := client.Data()
        if err != nil {
                log.Fatalf("Error getting data writer: %v", err)
        }
        _, err = writer.Write(message)
        if err != nil {
                log.Fatalf("Error writing message: %v", err)
        }
        err = writer.Close()
        if err != nil {
                log.Fatalf("Error closing writer: %v", err)
        }

        // Close the connection.
        err = client.Quit()
        if err != nil {
                log.Fatalf("Error quitting: %v", err)
        }

        log.Println("Automated email sent successfully!")
}

