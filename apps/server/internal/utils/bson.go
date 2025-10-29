package utils

import "go.mongodb.org/mongo-driver/bson"

func ToBsonSet[T any](dto *T) (bson.M, error) {
	data, err := bson.Marshal(dto)
	if err != nil {
		return nil, err
	}

	var set bson.M
	if err := bson.Unmarshal(data, &set); err != nil {
		return nil, err
	}

	return set, nil
}
