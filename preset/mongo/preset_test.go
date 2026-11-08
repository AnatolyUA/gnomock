package mongo_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/mongo"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodb "go.mongodb.org/mongo-driver/v2/mongo"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestPreset(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"7.0", "8.0", "8.3", "9.0"} {
		t.Run(version, testPreset(version))
	}
}

func testPreset(version string) func(t *testing.T) {
	return func(t *testing.T) {
		p := mongo.Preset(
			mongo.WithData("./testdata/"),
			mongo.WithUser("gnomock", "gnomick"),
			mongo.WithVersion(version),
		)
		c, err := gnomock.Start(p, gnomock.WithLogWriter(os.Stdout))

		defer func() { require.NoError(t, gnomock.Stop(c)) }()

		require.NoError(t, err)

		ctx := context.Background()
		addr := c.DefaultAddress()
		uri := fmt.Sprintf("mongodb://%s:%s@%s", "gnomock", "gnomick", addr)
		clientOptions := mongooptions.Client().ApplyURI(uri)

		client, err := mongodb.Connect(clientOptions)
		require.NoError(t, err)

		// see testdata folder to verify names/numbers
		count, err := client.Database("db1").Collection("users").CountDocuments(ctx, bson.D{})
		require.NoError(t, err)
		require.Equal(t, int64(10), count)

		count, err = client.Database("db2").Collection("customers").CountDocuments(ctx, bson.D{})
		require.NoError(t, err)
		require.Equal(t, int64(5), count)

		count, err = client.Database("db2").Collection("countries").CountDocuments(ctx, bson.D{})
		require.NoError(t, err)
		require.Equal(t, int64(3), count)

		// Extended JSON types are decoded into their BSON types.
		var event struct {
			ID   bson.ObjectID `bson:"_id"`
			At   time.Time     `bson:"at"`
			Seq  int64         `bson:"seq"`
			Tags []string      `bson:"tags"`
			Meta struct {
				Source string `bson:"source"`
			} `bson:"meta"`
		}

		events := client.Database("db2").Collection("events")
		require.NoError(t, events.FindOne(ctx, bson.D{{Key: "seq", Value: int64(9007199254740993)}}).Decode(&event))
		require.Equal(t, "5f1b2c3d4e5f6a7b8c9d0e1f", event.ID.Hex())
		require.Equal(t, time.Date(2020, 7, 24, 10, 0, 0, 0, time.UTC), event.At.UTC())
		require.Equal(t, []string{"a", "b"}, event.Tags)
		require.Equal(t, "gnomock", event.Meta.Source)

		count, err = events.CountDocuments(ctx, bson.D{{Key: "at", Value: event.At}})
		require.NoError(t, err)
		require.Equal(t, int64(2), count, "canonical and relaxed $date decode to the same value")

		require.NoError(t, client.Disconnect(ctx))
	}
}

func TestPreset_withDefaults(t *testing.T) {
	t.Parallel()

	p := mongo.Preset()
	c, err := gnomock.Start(p)
	require.NoError(t, err)

	defer func() { require.NoError(t, gnomock.Stop(c)) }()

	ctx := context.Background()
	addr := c.DefaultAddress()
	uri := fmt.Sprintf("mongodb://%s:%s@%s", "gnomock", "gnomick", addr)
	clientOptions := mongooptions.Client().ApplyURI(uri)

	client, err := mongodb.Connect(clientOptions)
	require.NoError(t, err)
	require.NoError(t, client.Disconnect(ctx))
}

func TestPreset_wrongDataFolder(t *testing.T) {
	t.Parallel()

	p := mongo.Preset(mongo.WithData("./bad-path"))
	c, err := gnomock.Start(p)
	require.Error(t, err)
	require.NoError(t, gnomock.Stop(c))
}
