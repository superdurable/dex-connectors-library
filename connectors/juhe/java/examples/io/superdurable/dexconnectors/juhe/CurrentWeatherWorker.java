// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.juhe;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.juhe.CurrentWeather.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the IP lookup Flow with the static juhe connection. */
public final class CurrentWeatherWorker {
    private CurrentWeatherWorker() {}

    /** Start a Worker with the API key supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String key = System.getenv("JUHE_API_KEY");
        if (key == null || key.isEmpty())
            throw new IllegalStateException("JUHE_API_KEY is required");
        WeatherFlow flow =
                new WeatherFlow(new GetCurrentWeatherOperation(call -> new SecretString(key)));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("juhe-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its current weather. */
    public static final class WeatherFlow implements Flow<GetCurrentWeatherInput> {
        private final GetCurrentWeatherOperation operation;

        /** Bind the operation during Worker registration. */
        public WeatherFlow(GetCurrentWeatherOperation operation) {
            this.operation = operation;
        }

        @Override
        public String getFlowType() {
            return "JavaJuheCurrentWeather";
        }

        @Override
        public StepList<GetCurrentWeatherInput> getSteps() {
            return StepList.startStep(new GetCurrentWeather(operation))
                    .otherSteps(new ReturnWeather());
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:current-weather group-label:"Current weather"
    // dex:explanation text:"Look up the current conditions for the requested city."
    public static final class GetCurrentWeather
            extends QueryStep<
                    GetCurrentWeatherInput,
                    GetCurrentWeatherInput,
                    GetCurrentWeatherOutput,
                    GetCurrentWeatherResult> {
        GetCurrentWeather(GetCurrentWeatherOperation operation) {
            super(
                    CurrentWeather.newGetCurrentWeatherStep(
                                    "GetCurrentWeather",
                                    GetCurrentWeatherInput.class,
                                    new StepAnnotations(
                                            "current-weather",
                                            "Current weather",
                                            "Fetch current conditions for the requested city."),
                                    operation,
                                    new ConnectionRef("juhe", "juhe"),
                                    input -> input)
                            .branch("fetched", ReturnWeather.class)
                            .build());
        }
    }

    // dex:group group-id:current-weather group-label:"Current weather"
    // dex:explanation text:"Complete with the weather fields returned by Juhe."
    public static final class ReturnWeather implements Step<GetCurrentWeatherResult> {
        @Override
        public Class<GetCurrentWeatherResult> getInputType() {
            return GetCurrentWeatherResult.class;
        }

        @Override
        public StepDecision execute(Context context, GetCurrentWeatherResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
