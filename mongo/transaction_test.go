package mongo

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"

	"github.com/avito-tech/go-transaction-manager/internal/mock"
	"github.com/avito-tech/go-transaction-manager/transaction"
	trmcontext "github.com/avito-tech/go-transaction-manager/transaction/context"
	"github.com/avito-tech/go-transaction-manager/transaction/manager"
	"github.com/avito-tech/go-transaction-manager/transaction/settings"
)

type user struct {
	ID primitive.ObjectID `bson:"_id,omitempty"`
}

func TestTransaction(t *testing.T) {
	t.Parallel()

	type args struct {
		ctx context.Context
	}

	testErr := errors.New("error test")
	doNil := func(mt *mtest.T, ctx context.Context) error {
		return nil
	}

	mt := mtest.New(
		t,
		mtest.NewOptions().ClientType(mtest.Mock),
	)
	defer mt.Close()

	tests := map[string]struct {
		prepare func(mt *mtest.T)
		args    args
		do      func(mt *mtest.T, ctx context.Context) error
		wantErr assert.ErrorAssertionFunc
	}{
		"success": {
			prepare: func(mt *mtest.T) {},
			args: args{
				ctx: context.Background(),
			},
			do:      doNil,
			wantErr: assert.NoError,
		},
		"commit_error": {
			prepare: func(mt *mtest.T) {},
			args: args{
				ctx: context.Background(),
			},
			do: func(mt *mtest.T, ctx context.Context) error {
				_, _ = mt.Coll.InsertOne(ctx, user{
					ID: primitive.NewObjectID(),
				})

				return nil
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				var divErr mongo.CommandError

				return assert.ErrorAs(t, err, &divErr) && assert.ErrorIs(t, err, transaction.ErrCommit)
			},
		},
		"rollback_after_error": {
			prepare: func(mt *mtest.T) {},
			args: args{
				ctx: context.Background(),
			},
			do: func(mt *mtest.T, ctx context.Context) error {
				s := mongo.SessionFromContext(ctx)

				require.NoError(mt, s.AbortTransaction(ctx))

				return testErr
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorIs(t, err, testErr) &&
					assert.ErrorIs(t, err, transaction.ErrRollback)
			},
		},
	}
	for name, tt := range tests {
		tt := tt
		mt.Run(name, func(mt *mtest.T) {
			mt.Parallel()

			log := mock.NewLog()

			tt.prepare(mt)

			s := settings.New(
				settings.WithPropagation(transaction.PropagationNested),
			)
			m := manager.New(
				NewDefaultFactory(mt.Client),
				manager.WithLog(log),
				manager.WithSettings(s),
			)

			var tr Transaction
			err := m.Do(tt.args.ctx, func(ctx context.Context) error {
				var trNested transaction.Transaction
				err := m.Do(ctx, func(ctx context.Context) error {
					trNested = trmcontext.DefaultManager.Default(ctx)

					require.NotNil(t, trNested)

					return tt.do(mt, ctx)
				})

				if trNested != nil {
					require.Equal(t, true, trNested.IsActive())
				}

				return err
			})
			require.Equal(t, false, tr.IsActive())

			if !tt.wantErr(t, err) {
				return
			}
		})
	}
}
