// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.qweather;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.qweather.CurrentWeather.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the weather lookup Flow with the static qweather connection. */
public final class CurrentWeatherWorker {
    private CurrentWeatherWorker() {}

    /** Start a Worker with the API key supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String host = System.getenv("QWEATHER_API_HOST");
        String key = System.getenv("QWEATHER_API_KEY");
        if (host == null || host.isEmpty() || key == null || key.isEmpty())
            throw new IllegalStateException("QWEATHER_API_HOST and QWEATHER_API_KEY are required");
        WeatherFlow flow =
                new WeatherFlow(
                        new GetCurrentWeatherOperation(call ->
                                new CurrentWeather.Credentials(
                                        new SecretString(host), new SecretString(key))));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("qweather-blobs").toString(),
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
            return "JavaQWeatherCurrentWeather";
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
    // dex:explanation text:"Look up the current conditions for the requested coordinates."
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
                                            "Fetch current conditions for the requested"
                                                    + " coordinates."),
                                    operation,
                                    new ConnectionRef("qweather", "qweather"),
                                    input -> input)
                            .branch("found", ReturnWeather.class)
                            .build());
        }
    }

    // dex:group group-id:current-weather group-label:"Current weather"
    // dex:explanation text:"Complete with the weather fields returned by QWeather."
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
